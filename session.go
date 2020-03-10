package main

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/securecookie"
	"github.com/jinzhu/gorm"
	"golang.org/x/crypto/bcrypt"
)

var (
	UserNotFound        = errors.New("找不到此帳號")
	WrongPassword       = errors.New("密碼錯誤")
	AccountAlreadyExist = errors.New("帳號已經存在")
)

type Session struct {
	ID       string
	ExpireAt time.Time
}

func init() {
	gob.Register(Session{})
}

func (h Handler) auth(next http.HandlerFunc, role Role) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if role == Unknown {
			next.ServeHTTP(w, r)
		} else if acct, ok := r.Context().Value(KeyAccount).(Account); ok && acct.Role <= role {
			next.ServeHTTP(w, r)
		} else {
			h.errorPage(w, r, 401, "權限不足", "您的權限不足，需要"+role.String()+"權限")
		}
	}
}

func (h Handler) identify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := securecookie.New(hashKey, blockKey)
		cookie, err := r.Cookie("session")
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		var session Session
		if err = s.Decode("session", cookie.Value, &session); err != nil {
			logout(w, r)
			next.ServeHTTP(w, r)
			return
		}
		if time.Now().After(session.ExpireAt) {
			// session expire
			logout(w, r)
			next.ServeHTTP(w, r)
			return
		}
		var acct Account
		if err = h.db.Set("gorm:auto_preload", true).First(&acct, "id = ?", session.ID).Error; err != nil {
			logout(w, r)
			next.ServeHTTP(w, r)
			return
		}
		ctx := r.Context()
		ctx = context.WithValue(ctx, KeyAccount, acct)
		r = r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
}

func newSession(id string) *http.Cookie {
	s := securecookie.New(hashKey, blockKey)
	var encoded string
	var err error
	session := Session{
		ID:       id,
		ExpireAt: expire(),
	}
	if encoded, err = s.Encode("session", session); err != nil {
		panic(err)
	}
	return &http.Cookie{
		Name:    "session",
		Value:   encoded,
		Path:    "/",
		Expires: session.ExpireAt,
	}
}

func expire() time.Time {
	return time.Now().AddDate(0, 0, 7)
}

func (h Handler) login(w http.ResponseWriter, r *http.Request) {
	var acct Account
	fmt.Println("username:", r.FormValue("username"))
	err := h.db.Where("id = ?", r.FormValue("username")).First(&acct).Error
	if gorm.IsRecordNotFoundError(err) {
		http.Error(w, UserNotFound.Error(), 404)
		return
	} else if err != nil {
		panic(err)
	}
	password := r.FormValue("password")
	if bcrypt.CompareHashAndPassword(acct.Password, []byte(password)) != nil {
		http.Error(w, WrongPassword.Error(), 403)
		return
	}
	http.SetCookie(w, newSession(acct.ID))
}

func logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		cookie.MaxAge = -1
		http.SetCookie(w, cookie)
	}
	http.Redirect(w, r, "/", 303)
}

func (h Handler) register(w http.ResponseWriter, r *http.Request) {
	next := func(msg string) {
		h.HTML(w, r, "register.htm", msg)
	}

	var err error
	var acct Account
	acct.ID = r.FormValue("account_id")
	if err = h.db.First(&acct, "id = ?", acct.ID).Error; !gorm.IsRecordNotFoundError(err) {
		next(AccountAlreadyExist.Error())
		return
	}
	acct.Name = r.FormValue("name")
	acct.Role, err = parseRole(r.FormValue("role"))
	if err != nil {
		next(fmt.Sprintf("身份 '%s' 無法被解析", r.FormValue("role")))
		return
	}

	var class Class
	if err = h.db.FirstOrCreate(&class, Class{Name: r.FormValue("class")}).Error; err != nil {
		next("無法建立班級：" + err.Error())
		return
	}
	acct.Class = class

	if acct.Role == Student {
		acct.Number, err = strconv.Atoi(r.FormValue("number"))
		if err != nil {
			next("座號不是數字")
			return
		}
	}

	acct.Password = generatePassword(r.FormValue("password"))

	if err = h.db.Create(&acct).Error; err != nil {
		next("無法註冊使用者：" + err.Error())
		return
	}

	next("成功註冊" + acct.Name)
}

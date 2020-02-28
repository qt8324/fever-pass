package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/jinzhu/gorm"
)

var (
	PermissionDenied = errors.New("permission denied")
	RecordNotFound   = errors.New("record not found")
)

type TempType uint32

const (
	UnknownType TempType = iota
	Ear
	Forehead
)

func (t TempType) String() string {
	switch t {
	case Ear:
		return "耳溫"

	case Forehead:
		return "額溫"
	}
	return ""
}

func parseType(str string) (TempType, error) {
	tempType, err := strconv.Atoi(str)
	if err != nil || tempType < 0 || tempType > 3 {
		return UnknownType, fmt.Errorf("Cannot parse '%s' as role", str)
	}
	return TempType(tempType), nil
}

type Record struct {
	gorm.Model

	Temperature float64
	Type        TempType

	Account    Account
	AccountID  string
	RecordedBy Account `gorm:"foreignkey:RecorderID"`
	RecorderID string
}

func (r Record) Fever() bool {
	switch r.Type {
	case Forehead:
		return r.Temperature >= 37.5

	case Ear:
		return r.Temperature >= 38
	}
	return false
}

// insert a new record into database
func (h Handler) newRecord(w http.ResponseWriter, r *http.Request) {
	var err error
	var account Account

	acct := r.Context().Value(KeyAccount).(Account)
	err = h.db.Joins(
		"JOIN classes ON class_id = classes.id",
	).Where(
		"classes.name = ? and number = ?", r.FormValue("class"), r.FormValue("number"),
	).First(&account).Error
	if gorm.IsRecordNotFoundError(err) {
		http.Error(w, "account not found", 404)
		return
	} else if err != nil {
		panic(err)
	}

	if !permission(acct, account) {
		http.Error(w, "permission denied", 403)
		return
	}

	record := Record{
		Account:    account,
		RecordedBy: acct,
	}

	record.Temperature, err = strconv.ParseFloat(r.FormValue("temperature"), 64)
	if err != nil {
		http.Error(w, err.Error(), 415)
		return
	}

	record.Type, err = parseType(r.FormValue("type"))
	if err != nil {
		http.Error(w, err.Error(), 415)
		return
	}

	err = h.db.Create(&record).Error
	if err != nil {
		panic(err)
	}

	if err = h.tpls["new.htm"].ExecuteTemplate(w, "row", record); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

// permission return whether A can modify B
func permission(a, b Account) bool {
	switch a.Role {
	case Admin:
		return true

	case Teacher:
		return a.ClassID == b.ClassID

	case Student:
		return a.ID == b.ID
	}
	return false
}

func (h Handler) listRecord(acct Account) (tx *gorm.DB) {
	tx = h.db.Order("id desc").Set("gorm:auto_preload", true)
	switch acct.Role {
	case Admin:
		return tx

	case Teacher:
		return tx.Joins(
			"JOIN accounts on records.account_id = accounts.id",
		).Where(
			"accounts.class_id = ?", acct.ClassID,
		)

	case Student:
		return tx.Where("account_id = ?", acct.ID)
	}

	return nil
}

func (h Handler) deleteRecord(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), 415)
		return
	}

	var record Record
	err = h.db.Preload("Account").First(&record, id).Error
	if gorm.IsRecordNotFoundError(err) {
		http.Error(w, RecordNotFound.Error(), 404)
	} else if err != nil {
		panic(err)
	}

	acct := r.Context().Value(KeyAccount).(Account)
	if !permission(acct, record.Account) {
		http.Error(w, PermissionDenied.Error(), 403)
		return
	}

	err = h.db.Delete(&record).Error
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
}

func (h Handler) newSelfRecord(w http.ResponseWriter, r *http.Request) {
	acct, ok := r.Context().Value(KeyAccount).(Account)
	if !ok {
		http.Error(w, "cannot read account from session", 500)
		return
	}

	record := Record{
		Account:    acct,
		RecordedBy: acct,
	}

	var err error
	record.Temperature, err = strconv.ParseFloat(r.FormValue("temperature"), 64)
	if err != nil {
		http.Error(w, err.Error(), 415)
		return
	}

	record.Type, err = parseType(r.FormValue("type"))
	if err != nil {
		http.Error(w, err.Error(), 415)
		return
	}

	err = h.db.Create(&record).Error
	if err != nil {
		panic(err)
	}

	h.index(w, r)
}

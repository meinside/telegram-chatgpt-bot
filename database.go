package main

import (
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Prompt struct
type Prompt struct {
	gorm.Model

	ChatID   int64 `gorm:"index"`
	UserID   int64
	Username string

	Text   string
	Tokens uint `gorm:"index"`

	Result Generated
}

// Generated struct
type Generated struct {
	gorm.Model

	Successful bool `gorm:"index"`
	Text       string
	Tokens     uint `gorm:"index"`

	PromptID int64 // foreign key
}

// Database struct
type Database struct {
	db *gorm.DB
}

// OpenDatabase opens and returns a database at given path: `dbPath`.
func OpenDatabase(dbPath string) (database *Database, err error) {
	var db *gorm.DB
	db, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		PrepareStmt: true,
	})

	if err == nil {
		// migrate tables
		if err := db.AutoMigrate(
			&Prompt{},
			&Generated{},
		); err != nil {
			log.Printf("failed to migrate databases: %s", err)
		}

		return &Database{db: db}, nil
	}

	return nil, err
}

// SavePrompt saves `prompt`.
func (d *Database) SavePrompt(prompt Prompt) (err error) {
	tx := d.db.Save(&prompt)
	return tx.Error
}

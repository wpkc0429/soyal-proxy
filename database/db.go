package database

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"soyal-proxy/cli"
)

type AccessLog struct {
	ID         uint      `gorm:"primaryKey"`
	Time       time.Time `gorm:"index"`
	DeviceName string
	CardID     string    `gorm:"index"`
	EventCode  int
	EventDesc  string
}

type User struct {
	ID          uint   `gorm:"primaryKey"`
	CardID      string `gorm:"uniqueIndex"`
	Notes       string
	Permissions string // JSON encoded map[string]config.PermissionData
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

var DB *gorm.DB

func InitDB(dbPath string, jsonPath string) error {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return err
	}

	err = db.AutoMigrate(&AccessLog{}, &User{})
	if err != nil {
		return err
	}

	DB = db

	// 如果資料庫是空的，嘗試從舊的 users.json 遷移資料（自動移植白名單）
	var count int64
	db.Model(&User{}).Count(&count)
	if count == 0 {
		migrateFromJSON(jsonPath)
	}

	return nil
}

func migrateFromJSON(jsonPath string) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return // File might not exist yet, that's fine
	}
	var users []cli.GlobalUser
	if err := json.Unmarshal(data, &users); err != nil {
		log.Printf("Failed to unmarshal %s for migration: %v", jsonPath, err)
		return
	}

	log.Printf("Migrating %d users from %s to SQLite...", len(users), jsonPath)
	for _, u := range users {
		permBytes, _ := json.Marshal(u.Permissions)
		dbUser := User{
			CardID:      u.CardID,
			Notes:       u.Notes,
			Permissions: string(permBytes),
		}
		DB.Create(&dbUser)
	}
	log.Println("Migration complete. You can now safely ignore users.json (system will use events.db)")
}

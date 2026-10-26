package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal("could not load .env file: ", err)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set. Make sure .env file exists and is populated.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := openDatabase(dsn)
	if err != nil {
		log.Fatal("database connection failed: ", err)
	}
	defer db.Close()

	if err := createTables(ctx, db); err != nil {
		log.Fatal("database schema initialization failed: ", err)
	}

	log.Print("connected to database and initialized schema")

}

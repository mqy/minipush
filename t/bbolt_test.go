package t

import (
	"log"
	"testing"

	"go.etcd.io/bbolt"
)

func Test(t *testing.T) {
	// Open the my.db data file in your current directory.
	// It will be created if it doesn't exist.
	db, err := bbolt.Open("my.db", 0600, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
}

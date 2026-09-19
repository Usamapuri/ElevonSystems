package main

import (
	"os"
	"strings"
	"testing"
)

// Pins two boot facts that types cannot express:
//  1. migrations run before the first-admin bootstrap (the INSERT needs users);
//  2. SetTrustedProxies(nil) stays, so c.ClientIP() is the TCP peer.
func TestMain_BootOrderAndTrustedProxies(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	mig := strings.Index(s, "database.Migrate(db)")
	adm := strings.Index(s, "database.EnsureInitialAdmin(db)")
	if mig < 0 || adm < 0 {
		t.Fatal("main.go must call database.Migrate(db) and database.EnsureInitialAdmin(db)")
	}
	if adm < mig {
		t.Fatal("EnsureInitialAdmin must run after Migrate")
	}
	if !strings.Contains(s, "router.SetTrustedProxies(nil)") {
		t.Fatal("main.go must keep router.SetTrustedProxies(nil)")
	}
}

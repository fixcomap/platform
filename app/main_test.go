package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestHandlerRoot(t *testing.T) {
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandlerNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDbcheckWithoutConfig(t *testing.T) {
	t.Setenv("DATABASE_URL_FILE", "")
	rec := httptest.NewRecorder()
	dbcheck(rec, httptest.NewRequest(http.MethodGet, "/dbcheck", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestDbcheckUnreachable(t *testing.T) {
	f := t.TempDir() + "/database-url"
	if err := os.WriteFile(f, []byte("postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL_FILE", f)
	rec := httptest.NewRecorder()
	dbcheck(rec, httptest.NewRequest(http.MethodGet, "/dbcheck", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

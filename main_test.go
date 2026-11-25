package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestNextDate(t *testing.T) {
	for _, c := range []struct{ day, recurrence, want string }{{"2024-01-31", "monthly", "2024-02-29"}, {"2025-01-31", "monthly", "2025-02-28"}, {"2026-12-31", "daily", "2027-01-01"}, {"2026-11-25", "weekly", "2026-12-02"}} {
		if got := nextDate(c.day, c.recurrence); got != c.want {
			t.Errorf("nextDate(%s, %s) = %s; want %s", c.day, c.recurrence, got, c.want)
		}
	}
}
func TestApplyRecurrenceAndValidation(t *testing.T) {
	task := newTask("test-task-1", "2026-11-25")
	title, repeat, done := "Review notes", "daily", true
	if err := apply(&task, Patch{Title: &title, Recurrence: &repeat, Completed: &done}, "2026-11-25"); err != nil {
		t.Fatal(err)
	}
	if task.NextOccurrence != "2026-11-26" {
		t.Fatal(task.NextOccurrence)
	}
	bad := []string{"javascript:alert(1)"}
	if apply(&task, Patch{Links: &bad}, "2026-11-25") == nil {
		t.Fatal("unsafe link accepted")
	}
}
func TestGzipNegotiation(t *testing.T) {
	r := httptest.NewRequest("GET", "http://localhost/", nil)
	r.Header.Set("Accept-Encoding", "br, gzip;q=0")
	if acceptsGzip(r) {
		t.Fatal("gzip disabled")
	}
	r.Header.Set("Accept-Encoding", "gzip, br")
	if !acceptsGzip(r) {
		t.Fatal("gzip expected")
	}
}
func TestAPI(t *testing.T) {
	db, e := openDB(filepath.Join(t.TempDir(), "tasks.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	a := &app{db: db, htmlGzip: compress(page)}
	call := func(method, path, body string, code int) State {
		t.Helper()
		r := httptest.NewRequest(method, "http://localhost:8080"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != code {
			t.Fatalf("%s %s: got %d, want %d: %s", method, path, w.Code, code, w.Body.String())
		}
		var s State
		if code == 200 {
			if e := json.Unmarshal(w.Body.Bytes(), &s); e != nil {
				t.Fatal(e)
			}
		}
		return s
	}
	for _, id := range []string{"test-id-0001", "test-id-0002", "test-id-0003"} {
		call(http.MethodPost, "/api/tasks", `{"id":"`+id+`","title":"Focus task","focus":true}`, 200)
	}
	call(http.MethodPost, "/api/tasks", `{"id":"test-id-0004","title":"Fourth","focus":true}`, 409)
	s := call(http.MethodPost, "/api/tasks", `{"id":"test-id-0001","title":"Duplicate"}`, 200)
	if len(s.Tasks) != 3 {
		t.Fatal("duplicate create changed task count")
	}
	call(http.MethodPatch, "/api/tasks/test-id-0001", `{"recurrence":"daily","completed":true,"subtasks":[{"text":"one","done":true}]}`, 200)
	if _, e = db.Exec(`UPDATE tasks SET next_occurrence='2000-01-01' WHERE id='test-id-0001'`); e != nil {
		t.Fatal(e)
	}
	s, e = a.state(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	for _, task := range s.Tasks {
		if task.ID == "test-id-0001" && (task.Completed || task.Subtasks[0].Done || task.PlannedDate != s.Today) {
			t.Fatalf("bad reset: %+v", task)
		}
	}
	s = call(http.MethodDelete, "/api/tasks/test-id-0002", "", 200)
	if len(s.Tasks) != 2 {
		t.Fatal("delete failed")
	}
	r := httptest.NewRequest("POST", "http://localhost:8080/api/tasks", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://other.example")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin write accepted")
	}
}

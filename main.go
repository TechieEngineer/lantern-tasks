package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

//go:embed static/index.html
var page []byte

const dateLayout = "2006-01-02"
const columns = `id,title,priority,due_date,planned_date,estimate,category,tags,notes,links,subtasks,recurrence,rollover,completed,focus_date,next_occurrence,created_at,updated_at`

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,80}$`)
var errFocus = errors.New("Only three tasks can be in today's focus.")

type Subtask struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}
type Task struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Priority       string    `json:"priority"`
	DueDate        string    `json:"due_date"`
	PlannedDate    string    `json:"planned_date"`
	Estimate       int       `json:"estimate"`
	Category       string    `json:"category"`
	Tags           []string  `json:"tags"`
	Notes          string    `json:"notes"`
	Links          []string  `json:"links"`
	Subtasks       []Subtask `json:"subtasks"`
	Recurrence     string    `json:"recurrence"`
	Rollover       bool      `json:"rollover"`
	Completed      bool      `json:"completed"`
	FocusDate      string    `json:"focus_date"`
	NextOccurrence string    `json:"next_occurrence"`
	CreatedAt      string    `json:"created_at"`
	UpdatedAt      string    `json:"updated_at"`
}
type Patch struct {
	ID          string     `json:"id,omitempty"`
	Title       *string    `json:"title"`
	Priority    *string    `json:"priority"`
	DueDate     *string    `json:"due_date"`
	PlannedDate *string    `json:"planned_date"`
	Estimate    *int       `json:"estimate"`
	Category    *string    `json:"category"`
	Tags        *[]string  `json:"tags"`
	Notes       *string    `json:"notes"`
	Links       *[]string  `json:"links"`
	Subtasks    *[]Subtask `json:"subtasks"`
	Recurrence  *string    `json:"recurrence"`
	Rollover    *bool      `json:"rollover"`
	Completed   *bool      `json:"completed"`
	Focus       *bool      `json:"focus"`
}
type State struct {
	Today string `json:"today"`
	Tasks []Task `json:"tasks"`
}
type scanner interface{ Scan(...any) error }
type app struct {
	db       *sql.DB
	htmlGzip []byte
}

func today() string       { return time.Now().Format(dateLayout) }
func stamp() string       { return time.Now().UTC().Format(time.RFC3339Nano) }
func packed(v any) string { b, _ := json.Marshal(v); return string(b) }
func scanTask(s scanner) (Task, error) {
	var t Task
	var tags, links, subs string
	err := s.Scan(&t.ID, &t.Title, &t.Priority, &t.DueDate, &t.PlannedDate, &t.Estimate, &t.Category, &tags, &t.Notes, &links, &subs, &t.Recurrence, &t.Rollover, &t.Completed, &t.FocusDate, &t.NextOccurrence, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return t, err
	}
	if err = json.Unmarshal([]byte(tags), &t.Tags); err != nil {
		return t, err
	}
	if err = json.Unmarshal([]byte(links), &t.Links); err != nil {
		return t, err
	}
	if err = json.Unmarshal([]byte(subs), &t.Subtasks); err != nil {
		return t, err
	}
	if t.Tags == nil {
		t.Tags = []string{}
	}
	if t.Links == nil {
		t.Links = []string{}
	}
	if t.Subtasks == nil {
		t.Subtasks = []Subtask{}
	}
	return t, nil
}
func values(t Task) []any {
	return []any{t.ID, t.Title, t.Priority, t.DueDate, t.PlannedDate, t.Estimate, t.Category, packed(t.Tags), t.Notes, packed(t.Links), packed(t.Subtasks), t.Recurrence, t.Rollover, t.Completed, t.FocusDate, t.NextOccurrence, t.CreatedAt, t.UpdatedAt}
}
func newTask(id, day string) Task {
	return Task{ID: id, Priority: "Medium", PlannedDate: day, Tags: []string{}, Links: []string{}, Subtasks: []Subtask{}, Recurrence: "none", Rollover: true, CreatedAt: stamp()}
}
func nextDate(day, rec string) string {
	d, e := time.Parse(dateLayout, day)
	if e != nil {
		return ""
	}
	switch rec {
	case "daily":
		return d.AddDate(0, 0, 1).Format(dateLayout)
	case "weekly":
		return d.AddDate(0, 0, 7).Format(dateLayout)
	case "monthly":
		first := time.Date(d.Year(), d.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		last := first.AddDate(0, 1, -1).Day()
		n := d.Day()
		if n > last {
			n = last
		}
		return first.AddDate(0, 0, n-1).Format(dateLayout)
	}
	return ""
}
func validDate(s string) bool {
	if s == "" {
		return true
	}
	d, e := time.Parse(dateLayout, s)
	return e == nil && d.Year() >= 1 && d.Format(dateLayout) == s
}
func apply(t *Task, p Patch, day string) error {
	oldDone, oldRec := t.Completed, t.Recurrence
	if p.Title != nil {
		t.Title = strings.TrimSpace(*p.Title)
	}
	if p.Priority != nil {
		t.Priority = *p.Priority
	}
	if p.DueDate != nil {
		t.DueDate = *p.DueDate
	}
	if p.PlannedDate != nil {
		t.PlannedDate = *p.PlannedDate
	}
	if p.Estimate != nil {
		t.Estimate = *p.Estimate
	}
	if p.Category != nil {
		t.Category = strings.TrimSpace(*p.Category)
	}
	if p.Tags != nil {
		t.Tags = *p.Tags
	}
	if p.Notes != nil {
		t.Notes = *p.Notes
	}
	if p.Links != nil {
		t.Links = *p.Links
	}
	if p.Subtasks != nil {
		t.Subtasks = *p.Subtasks
	}
	if p.Recurrence != nil {
		t.Recurrence = *p.Recurrence
	}
	if p.Rollover != nil {
		t.Rollover = *p.Rollover
	}
	if p.Completed != nil {
		t.Completed = *p.Completed
	}
	if p.Focus != nil {
		if *p.Focus {
			t.FocusDate = day
		} else {
			t.FocusDate = ""
		}
	}
	if t.Title == "" || len(t.Title) > 500 {
		return errors.New("Title must contain 1–500 bytes.")
	}
	if t.Priority != "High" && t.Priority != "Medium" && t.Priority != "Low" {
		return errors.New("Invalid priority.")
	}
	if !validDate(t.DueDate) || !validDate(t.PlannedDate) {
		return errors.New("Invalid date.")
	}
	if t.Estimate < 0 || t.Estimate > 10080 {
		return errors.New("Estimate must be 0–10080 minutes.")
	}
	if len(t.Category) > 200 || len(t.Notes) > 20000 || len(t.Tags) > 30 || len(t.Links) > 20 || len(t.Subtasks) > 100 {
		return errors.New("Task details are too long.")
	}
	for _, v := range t.Tags {
		if len(v) > 100 {
			return errors.New("Tag too long.")
		}
	}
	for _, v := range t.Links {
		u, e := url.Parse(v)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || len(v) > 2000 {
			return errors.New("Links must be http:// or https:// URLs.")
		}
	}
	for _, v := range t.Subtasks {
		if strings.TrimSpace(v.Text) == "" || len(v.Text) > 500 {
			return errors.New("Invalid subtask text.")
		}
	}
	switch t.Recurrence {
	case "none", "daily", "weekly", "monthly":
	default:
		return errors.New("Invalid recurrence.")
	}
	if !t.Completed || t.Recurrence == "none" {
		t.NextOccurrence = ""
	} else if !oldDone || oldRec != t.Recurrence || t.NextOccurrence == "" {
		anchor := day
		if t.PlannedDate > anchor {
			anchor = t.PlannedDate
		}
		t.NextOccurrence = nextDate(anchor, t.Recurrence)
	}
	t.UpdatedAt = stamp()
	return nil
}

// Reset each due recurring task and roll unfinished planned dates forward, preserving deadlines.
func maintain(ctx context.Context, tx *sql.Tx, day string) error {
	now := stamp()
	_, e := tx.ExecContext(ctx, `UPDATE tasks SET completed=0, planned_date=next_occurrence, due_date=CASE WHEN due_date<>'' THEN next_occurrence ELSE '' END, subtasks=COALESCE((SELECT json_group_array(json_object('text',json_extract(value,'$.text'),'done',json('false'))) FROM json_each(tasks.subtasks)), '[]'), next_occurrence='',focus_date='',updated_at=? WHERE completed=1 AND recurrence<>'none' AND next_occurrence<>'' AND next_occurrence<=?`, now, day)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE tasks SET planned_date=?,updated_at=? WHERE completed=0 AND rollover=1 AND planned_date<>'' AND planned_date<?`, day, now, day)
	return e
}
func readState(ctx context.Context, tx *sql.Tx, day string) (State, error) {
	s := State{Today: day, Tasks: []Task{}}
	rows, e := tx.QueryContext(ctx, `SELECT `+columns+` FROM tasks ORDER BY created_at,id`)
	if e != nil {
		return s, e
	}
	defer rows.Close()
	for rows.Next() {
		t, e := scanTask(rows)
		if e != nil {
			return s, e
		}
		s.Tasks = append(s.Tasks, t)
	}
	return s, rows.Err()
}
func (a *app) state(ctx context.Context) (State, error) {
	day := today()
	tx, e := a.db.BeginTx(ctx, nil)
	if e != nil {
		return State{}, e
	}
	defer tx.Rollback()
	if e = maintain(ctx, tx, day); e != nil {
		return State{}, e
	}
	s, e := readState(ctx, tx, day)
	if e != nil {
		return s, e
	}
	return s, tx.Commit()
}
func (a *app) mutate(ctx context.Context, method, id string, p Patch) (State, int, error) {
	day := today()
	tx, e := a.db.BeginTx(ctx, nil)
	if e != nil {
		return State{}, 500, e
	}
	defer tx.Rollback()
	if e = maintain(ctx, tx, day); e != nil {
		return State{}, 500, e
	}
	if method == http.MethodDelete {
		if _, e = tx.ExecContext(ctx, `DELETE FROM tasks WHERE id=?`, id); e != nil {
			return State{}, 500, e
		}
	} else {
		t, e := scanTask(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM tasks WHERE id=?`, id))
		exists := e == nil
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return State{}, 500, e
		}
		if !exists && method == http.MethodPatch {
			return State{}, 404, errors.New("Task no longer exists.")
		}
		// Replaying a create after a lost response is harmless.
		if !(exists && method == http.MethodPost) {
			if !exists {
				t = newTask(id, day)
			}
			if e = apply(&t, p, day); e != nil {
				return State{}, 400, e
			}
			if t.FocusDate == day {
				var count int
				if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE focus_date=? AND id<>?`, day, id).Scan(&count); e != nil {
					return State{}, 500, e
				}
				if count >= 3 {
					return State{}, 409, errFocus
				}
			}
			if exists {
				v := values(t)
				args := append(append([]any{}, v[1:]...), id)
				_, e = tx.ExecContext(ctx, `UPDATE tasks SET title=?,priority=?,due_date=?,planned_date=?,estimate=?,category=?,tags=?,notes=?,links=?,subtasks=?,recurrence=?,rollover=?,completed=?,focus_date=?,next_occurrence=?,created_at=?,updated_at=? WHERE id=?`, args...)
			} else {
				_, e = tx.ExecContext(ctx, `INSERT INTO tasks (`+columns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, values(t)...)
			}
			if e != nil {
				return State{}, 500, e
			}
		}
	}
	if e = maintain(ctx, tx, day); e != nil {
		return State{}, 500, e
	}
	s, e := readState(ctx, tx, day)
	if e != nil {
		return State{}, 500, e
	}
	if e = tx.Commit(); e != nil {
		return State{}, 500, e
	}
	return s, 200, nil
}
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		bits := strings.Split(part, ";")
		if strings.TrimSpace(bits[0]) != "gzip" {
			continue
		}
		for _, b := range bits[1:] {
			k, v, ok := strings.Cut(strings.TrimSpace(b), "=")
			if ok && k == "q" {
				q, e := strconv.ParseFloat(v, 64)
				if e != nil || q <= 0 {
					return false
				}
			}
		}
		return true
	}
	return false
}
func compress(b []byte) []byte {
	var buf bytes.Buffer
	z := gzip.NewWriter(&buf)
	_, _ = z.Write(b)
	_ = z.Close()
	return buf.Bytes()
}
func send(w http.ResponseWriter, r *http.Request, code int, kind, cache string, b, compressed []byte) {
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("Vary", "Accept-Encoding")
	if len(b) > 512 && acceptsGzip(r) {
		if compressed == nil {
			compressed = compress(b)
		}
		b = compressed
		w.Header().Set("Content-Encoding", "gzip")
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(code)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b)
	}
}
func jsonOut(w http.ResponseWriter, r *http.Request, code int, v any) {
	b, e := json.Marshal(v)
	if e != nil {
		code = 500
		b = []byte(`{"error":"Encoding failed."}`)
	}
	send(w, r, code, "application/json; charset=utf-8", "no-store", b, nil)
}
func fail(w http.ResponseWriter, r *http.Request, code int, e error) {
	msg := e.Error()
	if code >= 500 {
		log.Printf("request: %v", e)
		msg = "Database or server unavailable; retry shortly."
	}
	jsonOut(w, r, code, map[string]string{"error": msg})
}
func hostAllowed(host string) bool {
	h, _, e := net.SplitHostPort(host)
	if e != nil {
		h = host
	}
	h = strings.Trim(h, "[]")
	machine, _ := os.Hostname()
	return strings.EqualFold(h, "localhost") || net.ParseIP(h) != nil || (machine != "" && (strings.EqualFold(h, machine) || strings.EqualFold(h, machine+".local")))
}
func (a *app) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	if !hostAllowed(r.Host) {
		fail(w, r, 403, errors.New("Use localhost or the server's LAN IP."))
		return
	}
	if r.URL.Path == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		send(w, r, 200, "text/html; charset=utf-8", "private, max-age=60", page, a.htmlGzip)
		return
	}
	if r.URL.Path == "/api/state" && r.Method == http.MethodGet {
		s, e := a.state(r.Context())
		if e != nil {
			fail(w, r, 500, e)
			return
		}
		jsonOut(w, r, 200, s)
		return
	}
	if r.URL.Path != "/api/tasks" && !strings.HasPrefix(r.URL.Path, "/api/tasks/") {
		fail(w, r, 404, errors.New("Not found."))
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, e := url.Parse(origin)
		if e != nil || u.Scheme != "http" || !strings.EqualFold(u.Host, r.Host) {
			fail(w, r, 403, errors.New("Cross-origin writes are not allowed."))
			return
		}
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" || !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		fail(w, r, 415, errors.New("Use same-origin application/json requests."))
		return
	}
	var p Patch
	var id string
	if r.Method == http.MethodPost && r.URL.Path == "/api/tasks" || r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/tasks/") {
		r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if e := d.Decode(&p); e != nil {
			fail(w, r, 400, errors.New("Invalid JSON body."))
			return
		}
		if e := d.Decode(new(any)); e != io.EOF {
			fail(w, r, 400, errors.New("Expected one JSON object."))
			return
		}
	} else if !(r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/tasks/")) {
		fail(w, r, 405, errors.New("Method not allowed."))
		return
	}
	if r.Method == http.MethodPost {
		id = p.ID
	} else {
		id = strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	}
	if !idPattern.MatchString(id) {
		fail(w, r, 400, errors.New("Invalid task ID."))
		return
	}
	s, code, e := a.mutate(r.Context(), r.Method, id, p)
	if e != nil {
		fail(w, r, code, e)
		return
	}
	jsonOut(w, r, code, s)
}
func openDB(path string) (*sql.DB, error) {
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, q := range []string{"PRAGMA busy_timeout=5000", "PRAGMA journal_mode=WAL", "PRAGMA synchronous=NORMAL", schema} {
		if _, e = db.Exec(q); e != nil {
			db.Close()
			return nil, e
		}
	}
	return db, nil
}
func lanIPs() []string {
	var ips []string
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		as, _ := i.Addrs()
		for _, a := range as {
			ip, _, e := net.ParseCIDR(a.String())
			if e == nil && ip.To4() != nil && !ip.IsLinkLocalUnicast() && !ip.IsLoopback() {
				ips = append(ips, ip.String())
			}
		}
	}
	sort.Strings(ips)
	return ips
}
func main() {
	path := flag.String("db", "tasks.db", "SQLite file path")
	flag.Parse()
	db, e := openDB(*path)
	if e != nil {
		log.Fatal(e)
	}
	defer db.Close()
	listener, e := net.Listen("tcp4", "0.0.0.0:8080")
	if e != nil {
		log.Fatal(e)
	}
	fmt.Println("Lantern Tasks — local-first LAN planner")
	fmt.Println("Local: http://localhost:8080")
	ips := lanIPs()
	for _, ip := range ips {
		fmt.Printf("Network: http://%s:8080\n", ip)
	}
	if len(ips) == 0 {
		fmt.Println("Network: no active IPv4 LAN address detected")
	}
	fmt.Println("Trusted LAN only. Press Ctrl+C to stop.")
	srv := &http.Server{Handler: &app{db: db, htmlGzip: compress(page)}, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	log.Fatal(srv.Serve(listener))
}

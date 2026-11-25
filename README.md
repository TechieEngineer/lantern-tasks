# Lantern Tasks

A local-first, lightweight LAN task planner for phones, laptops and older computers. One Go executable serves a single HTML file and a shared SQLite database. No Docker, Node.js, npm, CGO, compiler toolchain for C, Python packages or cloud account required.

## Features

- Quick capture, Today / All / Upcoming / Completed views and three daily focus slots (enforced by the API).
- High, Medium and Low priorities, planned days, deadlines, time estimates, categories, tags, subtasks, notes and HTTP(S) links.
- Daily, weekly and monthly recurring tasks; automatic rollover of unfinished planned days; dark/light themes.
- In-memory SPA, immediate optimistic edits, localStorage cache, sessionStorage pending edits and quick-capture draft, background sync every 10 seconds while visible and on tab visibility/focus/online events. No full-page refresh when switching views.
- Embedded HTML, precompressed gzip HTML, gzip JSON when supported, short private HTML cache, no-store API, SQLite WAL and parameterized queries.
- Plain JavaScript and standalone CSS. Tailwind CSS 2.2.19 is optionally loaded from jsDelivr after first render; `?lite=1` skips it entirely. No external JS, fonts or icons. Use lite mode for the lowest overhead and no CDN traffic.

## Requirements

Go 1.23 or newer, Python 3.9+ with SQLite JSON functions (Python 3.11+ recommended for SQLite 3.38+), and a browser supporting fetch, Promise/async, CSS Grid and localStorage. Initial Go dependency installation needs internet access. The app itself works without internet in lite mode. The Python worker is optional when using the web UI.

## Run (Windows PowerShell)

Extract the ZIP, then open PowerShell in the extracted `lantern-tasks` folder:

```powershell
go version
python --version
go mod tidy
$env:CGO_ENABLED = "0"
go test ./...
go run .
```

If `python` is not recognized but the Windows launcher is installed, use `py -3` in its place. `go mod tidy` downloads the pinned pure-Go SQLite driver and generates `go.sum` and any needed indirect dependency entries. Commit both module files after this succeeds. The server creates `tasks.db` automatically, listens on all IPv4 interfaces on port 8080, and prints:

```text
Local: http://localhost:8080
Network: http://<actual-interface-IP>:8080
```

Open `http://localhost:8080` on the host. On a second device connected to the same trusted Wi-Fi, open the real Network URL printed by Go. If more than one address is printed, choose the IP belonging to your Wi-Fi or Ethernet adapter (check `ipconfig` on Windows). Allow the Go program through Windows Firewall for **Private networks only** if prompted. Guest Wi-Fi client isolation, VPNs and firewall rules can block access. Stop the server with Ctrl+C. Do not port-forward 8080 or expose it to the internet.

For Linux/macOS, use `CGO_ENABLED=0 go run .` after `go mod tidy` and `go test ./...`; use `python3` for the worker.

### Build once, run faster

```powershell
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags="-s -w" -o lantern-tasks.exe .
.\lantern-tasks.exe
```

Go embeds `static/index.html` and `schema.sql` in the binary. The executable can run without Go installed, on a compatible OS/architecture. The database is created in the working directory; run from the same folder each time. A custom database path is supported with `-db "path/to/tasks.db"` (and `--db` before the Python command).

## Python CLI (a second terminal in the same folder)

```powershell
python worker.py add "Revise physics" --priority High --estimate 45 --category Study --tag revision
python worker.py add "Daily reading" --repeat daily
python worker.py add "Submit assignment" --due 2027-01-15 --planned 2027-01-14
python worker.py list
python worker.py maintain
python worker.py reset-recurring
python worker.py rollover
python worker.py --help
```

The script uses Python's standard library only (`sqlite3` for the database) and initializes the shared schema if needed. It uses the same database as Go, even while the server runs. The web UI picks up CLI edits on the next background sync or manual Sync. `--no-rollover` disables planned-day rollover for that task.

## Task behavior

- Planned day is when you intend to work; due date is the deadline. Unfinished tasks with rollover enabled and a past planned day move to today, without changing their deadline. An overdue deadline remains visibly overdue. Today also includes unscheduled and overdue unfinished tasks.
- Completing a recurring task schedules its next occurrence from the later of its planned day and the host's current date. Weekly means seven days; monthly clamps to the next month's last valid day if necessary (subsequent repeats use the resulting day). At the next occurrence the task and its subtasks reset; if it had a deadline, the deadline becomes that occurrence date. Missed occurrences do not create duplicates. The same task is reused, so this is not a historical completion log.
- Focus is scoped to the host's local calendar day; a completed focus task still occupies its slot until removed or the day changes. The server runs maintenance on API reads/writes; Python can run it independently when the server is stopped.
- The cache is per browser/origin. Pending edits and quick drafts survive a reload in the same tab when sessionStorage is available; they may be lost when the tab closes before sync. Offline updates retry with backoff; rejected edits are rolled back with a message. There is no service worker: offline reload is not guaranteed. Multiple clients sync through SQLite, not through WebSockets. Updates to different fields merge; simultaneous updates to the same field (including a subtask list) are last-write-wins. Browser focus triggers an immediate sync attempt, but OS suspension/network recovery can delay it.
- The server uses its own calendar and timezone for recurrence, focus and rollover. No external calendar integration or alarms are included.

## GitHub (PowerShell)

Create a new empty repository through GitHub CLI (`gh`) or the GitHub website. Keep the database private: `.gitignore` excludes `tasks.db`, WAL/SHM files, and build output. If using a custom database name, use a `.db` extension or explicitly ignore it.

```powershell
git init -b main
git add .
git commit -m "feat: build Lantern Tasks LAN planner"
gh auth login
gh repo create lantern-tasks --public --source=. --remote=origin --push
```

If the repository already exists, instead of `gh repo create`, run:

```powershell
git remote add origin https://github.com/YOUR_USERNAME/lantern-tasks.git
git push -u origin main
```

Replace `YOUR_USERNAME`; use `--private` instead of `--public` if preferred. GitHub Pages cannot run the Go backend; publish the source or release binaries, not the private live database.

## Second device

For a shared to-do list, **do not clone anything**: leave the server running on the first computer and visit the printed LAN address in the second device's browser (append `?lite=1` if desired). Both devices then use the same tasks.db.

To host a separate local instance for development:

```powershell
git clone https://github.com/YOUR_USERNAME/lantern-tasks.git
Set-Location lantern-tasks
go mod tidy
$env:CGO_ENABLED = "0"
go run .
```

This creates a separate database; cloning the source does not synchronize task data. To move an existing instance, stop the server and worker, then copy the database (and any remaining WAL/SHM files together if present) securely. For a live backup use SQLite's backup API rather than copying a running WAL database.

## Project layout

```text
lantern-tasks/
├── main.go           # HTTP server, API, validation, SQLite, LAN addresses
├── go.mod            # pinned pure-Go SQLite dependency
├── main_test.go      # unit and API/database tests
├── schema.sql        # shared database definition
├── worker.py         # standard-library CLI
├── static/
│   └── index.html    # embedded single-file UI
├── .gitignore        # database/build output protection
└── README.md         # setup, usage, limitations
```

## API

| Method | Path | Description |
|---|---|---|
| GET | `/api/state` | Current server day and all tasks; runs maintenance |
| POST | `/api/tasks` | Create (client-generated unique ID, title required) |
| PATCH | `/api/tasks/{id}` | Update specified task fields; `focus` selects/removes today's focus |
| DELETE | `/api/tasks/{id}` | Delete task |

All writes require `Content-Type: application/json`; API responses return the latest state on success. Task IDs are 8–80 alphanumeric/underscore/hyphen characters. Invalid inputs and a fourth focus selection are rejected. No API token is required.

## Security and verification

This is a personal, trusted-LAN MVP, **not a production multi-user service**. There is no authentication, per-user separation, HTTPS or encrypted database. Anyone who can reach the server can read or change its tasks. Host/origin checks, body limits, parameterized SQL, safe DOM text, allowed URL schemes and a Content Security Policy reduce common risks but do not replace authentication or encryption. Avoid storing secrets. Browser localStorage also holds task data, so avoid shared/public browsers. The optional CDN receives a stylesheet request; use `?lite=1` to eliminate it.

Packaging checks passed: JavaScript syntax (Node), Python syntax, Python CLI add/list and SQLite recurring reset, subtask reset and planned-day rollover; Go source compilation and pure logic tests using a temporary placeholder driver import. The packaging environment could not download modernc.org/sqlite because outbound DNS/network was blocked. Therefore the real Go driver build, Go database/API integration test, browser interactions and LAN testing are **not verified** here; `go.sum` is not included. Run `go mod tidy` and `go test ./...` locally before publishing. No placeholder driver or test database is included.

Manual acceptance test: add a task, edit its metadata, mark subtasks, set three focus tasks and verify a fourth is refused, complete a recurring task, reload, change theme, open on a second device and run `python worker.py add "Sync check"` in another terminal. Verify the new task appears after sync, then briefly disconnect a client and verify pending edits sync once reconnected. For a large number of tasks the entire state is still loaded and filtered in memory; pagination/virtualization and stronger conflict resolution are future improvements.

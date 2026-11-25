CREATE TABLE IF NOT EXISTS tasks (
 id TEXT PRIMARY KEY, title TEXT NOT NULL, priority TEXT NOT NULL DEFAULT 'Medium' CHECK(priority IN ('High','Medium','Low')),
 due_date TEXT NOT NULL DEFAULT '', planned_date TEXT NOT NULL DEFAULT '', estimate INTEGER NOT NULL DEFAULT 0,
 category TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '[]', notes TEXT NOT NULL DEFAULT '', links TEXT NOT NULL DEFAULT '[]',
 subtasks TEXT NOT NULL DEFAULT '[]', recurrence TEXT NOT NULL DEFAULT 'none' CHECK(recurrence IN ('none','daily','weekly','monthly')),
 rollover INTEGER NOT NULL DEFAULT 1, completed INTEGER NOT NULL DEFAULT 0, focus_date TEXT NOT NULL DEFAULT '',
 next_occurrence TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS tasks_planned ON tasks(completed, planned_date);
CREATE INDEX IF NOT EXISTS tasks_focus ON tasks(focus_date);

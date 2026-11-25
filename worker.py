#!/usr/bin/env python3
"""Lantern Tasks CLI; Python 3 standard library only."""
import argparse
import json
import sqlite3
import uuid
from datetime import date, datetime, timezone
from pathlib import Path
from contextlib import closing

SCHEMA = Path(__file__).with_name('schema.sql').read_text(encoding='utf-8')

def stamp():
    return datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')

def reset_recurring(db, day):
    return db.execute("""UPDATE tasks SET completed=0, planned_date=next_occurrence,
        due_date=CASE WHEN due_date<>'' THEN next_occurrence ELSE '' END,
        subtasks=COALESCE((SELECT json_group_array(json_object('text',json_extract(value,'$.text'),'done',json('false'))) FROM json_each(tasks.subtasks)), '[]'),
        next_occurrence='',focus_date='',updated_at=? WHERE completed=1 AND recurrence<>'none'
        AND next_occurrence<>'' AND next_occurrence<=?""", (stamp(), day)).rowcount

def rollover(db, day):
    return db.execute("""UPDATE tasks SET planned_date=?,updated_at=? WHERE completed=0
        AND rollover=1 AND planned_date<>'' AND planned_date<?""", (day, stamp(), day)).rowcount

def parse_date(value):
    if not value:
        return ""
    try:
        return date.fromisoformat(value).isoformat()
    except ValueError as exc:
        raise argparse.ArgumentTypeError('Use YYYY-MM-DD') from exc

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--db', default='tasks.db', help='Same database path as Go -db (default: tasks.db)')
    sub = parser.add_subparsers(dest='command', required=True)
    add = sub.add_parser('add', help='Add a task without opening the browser')
    add.add_argument('title')
    add.add_argument('--priority', choices=['High','Medium','Low'], default='Medium')
    add.add_argument('--due', type=parse_date, default='')
    add.add_argument('--planned', type=parse_date, default=date.today().isoformat())
    add.add_argument('--estimate', type=int, default=0, help='Minutes')
    add.add_argument('--category', default='')
    add.add_argument('--tag', action='append', default=[])
    add.add_argument('--notes', default='')
    add.add_argument('--repeat', choices=['none','daily','weekly','monthly'], default='none')
    add.add_argument('--no-rollover', action='store_true')
    sub.add_parser('list', help='List all tasks')
    sub.add_parser('maintain', help='Reset due recurrences and roll unfinished planned dates forward')
    sub.add_parser('reset-recurring', help='Reset due completed recurring tasks')
    sub.add_parser('rollover', help='Roll unfinished planned dates forward')
    args = parser.parse_args()
    day = date.today().isoformat()
    try:
        with closing(sqlite3.connect(args.db, timeout=5)) as db:
            db.execute('PRAGMA busy_timeout=5000')
            db.execute('PRAGMA journal_mode=WAL')
            db.executescript(SCHEMA)
            with db:
                if args.command == 'add':
                    title = args.title.strip()
                    if not title or len(title.encode()) > 500 or not 0 <= args.estimate <= 10080:
                        parser.error('Title must contain 1–500 bytes; estimate must be 0–10080.')
                    if len(args.category.encode())>200 or len(args.notes.encode())>20000 or len(args.tag)>30 or any(len(t.encode())>100 for t in args.tag):
                        parser.error('Category, notes or tags exceed limits.')
                    now = stamp()
                    db.execute('''INSERT INTO tasks(id,title,priority,due_date,planned_date,estimate,category,tags,notes,recurrence,rollover,created_at,updated_at)
                        VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)''', (uuid.uuid4().hex,title,args.priority,args.due,args.planned,args.estimate,args.category,json.dumps(args.tag),args.notes,args.repeat,int(not args.no_rollover),now,now))
                    print('Added:', title)
                elif args.command == 'list':
                    for row in db.execute('SELECT completed,priority,title,planned_date,due_date FROM tasks ORDER BY completed,planned_date,created_at'):
                        done, priority, title, planned, due = row
                        print(f'[{"x" if done else " "}] {priority:6} {title} | plan: {planned or "—"} | due: {due or "—"}')
                else:
                    if args.command in ('maintain','reset-recurring'):
                        print('Recurring tasks reset:', reset_recurring(db, day))
                    if args.command in ('maintain','rollover'):
                        print('Unfinished tasks rolled over:', rollover(db, day))
    except sqlite3.Error as exc:
        parser.exit(1, f'Database error: {exc}\n')

if __name__ == '__main__':
    main()

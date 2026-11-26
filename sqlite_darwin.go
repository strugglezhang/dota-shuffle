//go:build cgo

package main

/*
#cgo LDFLAGS: -lsqlite3
#include <sqlite3.h>
#include <stdlib.h>

static int bind_text(sqlite3_stmt *stmt, int index, const char *value, int size) {
	return sqlite3_bind_text(stmt, index, value, size, SQLITE_TRANSIENT);
}
*/
import "C"

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"unsafe"
)

func init() {
	sql.Register("sqlite", macSQLiteDriver{})
}

// macSQLiteDriver 为 Store 提供系统 SQLite 的执行与查询接口。
type macSQLiteDriver struct{}

func (macSQLiteDriver) Open(path string) (driver.Conn, error) {
	if strings.ContainsRune(path, 0) {
		return nil, errors.New("SQLite 数据库路径包含空字符")
	}
	name := C.CString(path)
	defer C.free(unsafe.Pointer(name))
	conn := &macSQLiteConn{}
	flags := C.SQLITE_OPEN_READWRITE | C.SQLITE_OPEN_CREATE | C.SQLITE_OPEN_URI
	if code := C.sqlite3_open_v2(name, &conn.db, C.int(flags), nil); code != C.SQLITE_OK {
		err := conn.sqliteError(code)
		C.sqlite3_close(conn.db)
		return nil, err
	}
	return conn, nil
}

type macSQLiteConn struct {
	db *C.sqlite3
}

var (
	_ driver.Execer  = (*macSQLiteConn)(nil)
	_ driver.Queryer = (*macSQLiteConn)(nil)
)

func (c *macSQLiteConn) Close() error {
	if code := C.sqlite3_close(c.db); code != C.SQLITE_OK {
		return c.sqliteError(code)
	}
	return nil
}

func (*macSQLiteConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("历史库使用单次执行的 SQL")
}

func (*macSQLiteConn) Begin() (driver.Tx, error) {
	return nil, errors.New("历史库使用 SQLite 的自动提交事务")
}

func (c *macSQLiteConn) Exec(query string, args []driver.Value) (driver.Result, error) {
	if len(args) == 0 {
		// 建表迁移会一次执行多条 DDL，sqlite3_prepare_v2 只编译其中第一条。
		text := C.CString(query)
		defer C.free(unsafe.Pointer(text))
		if code := C.sqlite3_exec(c.db, text, nil, nil, nil); code != C.SQLITE_OK {
			return nil, c.sqliteError(code)
		}
	} else {
		stmt, err := c.prepare(query, args)
		if err != nil {
			return nil, err
		}
		defer C.sqlite3_finalize(stmt)
		if code := C.sqlite3_step(stmt); code != C.SQLITE_DONE {
			return nil, c.sqliteError(code)
		}
	}
	return macSQLiteResult{
		id:      int64(C.sqlite3_last_insert_rowid(c.db)),
		changed: int64(C.sqlite3_changes(c.db)),
	}, nil
}

func (c *macSQLiteConn) Query(query string, args []driver.Value) (driver.Rows, error) {
	stmt, err := c.prepare(query, args)
	if err != nil {
		return nil, err
	}
	return &macSQLiteRows{conn: c, stmt: stmt}, nil
}

func (c *macSQLiteConn) prepare(query string, args []driver.Value) (*C.sqlite3_stmt, error) {
	text := C.CString(query)
	defer C.free(unsafe.Pointer(text))
	var stmt *C.sqlite3_stmt
	if code := C.sqlite3_prepare_v2(c.db, text, -1, &stmt, nil); code != C.SQLITE_OK {
		return nil, c.sqliteError(code)
	}
	if stmt == nil {
		return nil, errors.New("SQLite 语句为空")
	}
	if count := int(C.sqlite3_bind_parameter_count(stmt)); count != len(args) {
		C.sqlite3_finalize(stmt)
		return nil, fmt.Errorf("SQLite 需要 %d 个参数，收到 %d 个", count, len(args))
	}
	for i, arg := range args {
		index := C.int(i + 1)
		var code C.int
		switch value := arg.(type) {
		case int64:
			code = C.sqlite3_bind_int64(stmt, index, C.sqlite3_int64(value))
		case string:
			text := C.CString(value)
			code = C.bind_text(stmt, index, text, C.int(len(value)))
			C.free(unsafe.Pointer(text))
		default:
			C.sqlite3_finalize(stmt)
			return nil, fmt.Errorf("历史库不支持 SQLite 参数类型 %T", arg)
		}
		if code != C.SQLITE_OK {
			err := c.sqliteError(code)
			C.sqlite3_finalize(stmt)
			return nil, err
		}
	}
	return stmt, nil
}

func (c *macSQLiteConn) sqliteError(code C.int) error {
	if c.db == nil {
		return fmt.Errorf("SQLite (%d): %s", code, C.GoString(C.sqlite3_errstr(code)))
	}
	return fmt.Errorf("SQLite (%d): %s", code, C.GoString(C.sqlite3_errmsg(c.db)))
}

type macSQLiteResult struct {
	id, changed int64
}

func (r macSQLiteResult) LastInsertId() (int64, error) { return r.id, nil }
func (r macSQLiteResult) RowsAffected() (int64, error) { return r.changed, nil }

type macSQLiteRows struct {
	conn *macSQLiteConn
	stmt *C.sqlite3_stmt
}

func (r *macSQLiteRows) Columns() []string {
	names := make([]string, int(C.sqlite3_column_count(r.stmt)))
	for i := range names {
		names[i] = C.GoString(C.sqlite3_column_name(r.stmt, C.int(i)))
	}
	return names
}

func (r *macSQLiteRows) Close() error {
	if code := C.sqlite3_finalize(r.stmt); code != C.SQLITE_OK {
		return r.conn.sqliteError(code)
	}
	return nil
}

func (r *macSQLiteRows) Next(dest []driver.Value) error {
	switch code := C.sqlite3_step(r.stmt); code {
	case C.SQLITE_DONE:
		return io.EOF
	case C.SQLITE_ROW:
	default:
		return r.conn.sqliteError(code)
	}
	for i := range dest {
		col := C.int(i)
		switch C.sqlite3_column_type(r.stmt, col) {
		case C.SQLITE_INTEGER:
			dest[i] = int64(C.sqlite3_column_int64(r.stmt, col))
		case C.SQLITE_TEXT:
			value := C.sqlite3_column_text(r.stmt, col)
			dest[i] = C.GoStringN((*C.char)(unsafe.Pointer(value)), C.sqlite3_column_bytes(r.stmt, col))
		case C.SQLITE_NULL:
			dest[i] = nil
		default:
			return fmt.Errorf("历史库查询返回了不支持的 SQLite 类型（列 %d）", i)
		}
	}
	return nil
}

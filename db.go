package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
	log "github.com/sirupsen/logrus"
)

type DBConnection struct {
	app    *PlugApplication
	con    *sql.DB
	db_uri string
}

const SQL_CREATE_PLUGS = `CREATE TABLE plugs (
id              SERIAL PRIMARY KEY,
s3id            VARCHAR(64) NOT NULL,
owner           VARCHAR(32) NOT NULL,
views           INTEGER NOT NULL,
approved        BOOLEAN NOT NULL,
shape           TEXT    NOT NULL
);`

const SQL_CREATE_LOG_TABLE = `CREATE TABLE logs (
time            TIMESTAMP PRIMARY KEY,
severity        INTEGER NOT NULL,
message         TEXT NOT NULL
);`

const SQL_CREATE_PLUG = `INSERT into plugs (s3id, owner, views, approved, shape)
VALUES ($1::text, $2::text, $3::integer, false, $4::text)`

const SQL_RETRIEVE_APPROVED_PLUGS = `SELECT id, s3id, owner, views FROM plugs WHERE approved=true`

const SQL_RETRIEVE_APPROVED_BANNER_PLUGS = `SELECT id, s3id, owner, views FROM plugs WHERE approved=true AND shape='banner'`

const SQL_RETRIEVE_APPROVED_VERT_PLUGS = `SELECT id, s3id, owner, views FROM plugs WHERE approved=true AND shape='vert'`

const SQL_RETRIEVE_PLUG_BY_ID = `SELECT s3id, owner, views, approved FROM plugs WHERE id=$1::integer`

const SQL_RETRIEVE_PENDING_PLUGS = `SELECT id, s3id, owner, views, approved, shape FROM plugs WHERE views>=0`

const SQL_SET_PENDING_PLUGS = `UPDATE plugs
SET approved = true
WHERE $1::text LIKE CONCAT('%,',id,',%');`

const SQL_DELETE_PLUG = `DELETE from plugs WHERE id=$1::integer;`

const SQL_INSERT_LOG = `INSERT into logs (time, severity, message)
VALUES ($1, $2::integer, $3::text)`

func (c *DBConnection) Init(app *PlugApplication, db_uri string) {
	c.app = app
	c.db_uri = db_uri
	c.reconnectToDB()
	c.create_table_safe("plugs", SQL_CREATE_PLUGS)
	c.create_table_safe("logs", SQL_CREATE_LOG_TABLE)
}

func (c *DBConnection) reconnectToDB() {
	db_con, err := sql.Open("postgres", c.db_uri)
	if err != nil {
		log.Fatal("error connecting to db!")
	}
	c.con = db_con
}

func (c DBConnection) pingDBAlive() {
	err := c.con.Ping()
	if err != nil {
		log.Info("failed to ping db!")
		log.Info(err)
		c.reconnectToDB()
	}
}

func (c DBConnection) create_table_safe(name, sql string) {
	c.pingDBAlive()
	rows, err := c.con.Query("SELECT 1::integer FROM pg_tables WHERE schemaname = 'public' AND tablename = $1::text;",
		name)
	if err != nil {
		log.Error(err)
	}
	if !rows.Next() {
		_, err = c.con.Exec(sql)
		if err != nil {
			log.Fatal(err)
		}
	} else {
	}
}

func (c DBConnection) GetPlug(shape string) Plug {
	var rows *sql.Rows
	var err error
	switch shape {
	case "banner":
		rows, err = c.con.Query(SQL_RETRIEVE_APPROVED_BANNER_PLUGS)
	case "vert":
		rows, err = c.con.Query(SQL_RETRIEVE_APPROVED_VERT_PLUGS)
	}

	if err != nil {
		log.Fatal(err)
	}

	var plugs []Plug
	for rows.Next() {
		var obj Plug
		err = rows.Scan(&obj.ID, &obj.S3ID, &obj.Owner, &obj.ViewsRemaining)

		if err != nil {
			log.Error(err)
		}

		plugs = append(plugs, obj)
	}
	finalPlug := ChoosePlug(plugs)

	if finalPlug.ViewsRemaining > 0 {
		finalPlug.ViewsRemaining -= 1
		_, err = c.con.Exec("UPDATE plugs SET views=$2::integer WHERE id=$1::integer;",
			finalPlug.ID, finalPlug.ViewsRemaining)
		if err != nil {
			log.Error(err)
		}
	}
	if finalPlug.ViewsRemaining == 0 {
		c.DeletePlug(&finalPlug)
		// try again
		return c.GetPlug("banner")
	}

	return finalPlug
}

func (c DBConnection) GetPlugById(id int) (*Plug, error) {
	row := c.con.QueryRow(SQL_RETRIEVE_PLUG_BY_ID, id)

	obj := Plug{
		ID: id,
	}

	if err := row.Scan(&obj.S3ID, &obj.Owner, &obj.ViewsRemaining, &obj.Approved); err != nil {
		return nil, fmt.Errorf("failed to query for plug via ID %d: %w", id, err)
	}

	return &obj, nil
}

func (c DBConnection) DeletePlug(plug *Plug) error {
	_, err := c.con.Exec(SQL_DELETE_PLUG, plug.ID)
	if err != nil {
		return fmt.Errorf("failed to delete plug %v: %w", plug, err)
	}

	return c.app.s3.DelFile(plug)
}

func (c DBConnection) GetPendingPlugs() ([]*Plug, error) {
	var plugs []*Plug

	rows, err := c.con.Query(SQL_RETRIEVE_PENDING_PLUGS)

	if err != nil {
		return plugs, fmt.Errorf("failed to query for pending plugs: %w", err)
	}

	defer rows.Close()

	for rows.Next() {
		var obj *Plug
		err = rows.Scan(&obj.ID, &obj.S3ID, &obj.Owner, &obj.ViewsRemaining, &obj.Approved, &obj.Shape)

		if err != nil {
			return plugs, fmt.Errorf("failed to scan scan pending plug row: %w", err)
		}

		plugs = append(plugs, obj)
	}

	return plugs, nil
}

func (c DBConnection) GetUserPlugs(user string) ([]*Plug, error) {
	var plugs []Plug

	rows, err := c.con.Query(SQL_RETRIEVE_PENDING_PLUGS)

	if err != nil {
		return fmt.Errorf("failed to query for user %s's plugs: %w", user, error)
	}

	defer rows.Close()

	for rows.Next() {
		var obj *Plug
		err = rows.Scan(&obj.ID, &obj.S3ID, &obj.Owner, &obj.ViewsRemaining, &obj.Approved, &obj.Shape)

		if err != nil {
			return plugs, fmt.Errorf("failed to scan scan pending plug row: %w", err)
		}

		if obj.Owner == user {
			plugs = append(plugs, obj)
		}
	}

	return plugs
}

func (c DBConnection) SetPendingPlugs(approvedList []string) {
	_, err := c.con.Exec("UPDATE plugs SET approved = false;")
	if err != nil {
		log.Fatal(err)
	}

	stringList := "," + strings.Join(approvedList, ",") + ","
	_, err = c.con.Exec(SQL_SET_PENDING_PLUGS, stringList)

	if err != nil {
		log.Fatal(err)
	}
}

func (c DBConnection) AddLog(severity int, message string) {
	_, err := c.con.Exec(
		SQL_INSERT_LOG,
		time.Now(),
		severity,
		message)

	if err != nil {
		log.Error(err)
	}
}

func (c DBConnection) MakePlug(plug Plug) {
	_, err := c.con.Exec(
		SQL_CREATE_PLUG,
		plug.S3ID,
		plug.Owner,
		plug.ViewsRemaining,
		plug.Shape,
	)
	if err != nil {
		log.Error(err)
	}
}

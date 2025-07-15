// Copyright (c) 2019 The Jaeger Authors.
// Copyright (c) 2017 Uber Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package location

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/signadot/hotrod/pkg/config"
	"github.com/signadot/hotrod/pkg/delay"
	"github.com/signadot/hotrod/pkg/log"
	"github.com/signadot/hotrod/pkg/tracing"
)

// database implements a Location repository on top of an SQL database
type database struct {
	tracer trace.Tracer
	logger log.Factory
	lock   *tracing.Mutex
	db     *sqlx.DB
}

const tableSchema = `
CREATE TABLE IF NOT EXISTS locations
(
    id bigint unsigned NOT NULL AUTO_INCREMENT,
    name varchar(255) NOT NULL,
    coordinates varchar(255) NOT NULL,
    zone varchar(255) DEFAULT NULL,

    PRIMARY KEY (id),
	UNIQUE KEY name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
`

var seed = []Location{
	{ID: 1, Name: "My Home", Coordinates: "231,773", Zone: "green-zone"},
	{ID: 123, Name: "Rachel's Floral Designs", Coordinates: "115,277", Zone: "red-zone"},
	{ID: 567, Name: "Amazing Coffee Roasters", Coordinates: "211,653", Zone: "yellow-zone"},
	{ID: 392, Name: "Trom Chocolatier", Coordinates: "577,322", Zone: "purple-zone"},
	{ID: 731, Name: "Japanese Desserts", Coordinates: "728,326", Zone: "blue-zone"},
}

func newDatabase(logger log.Factory) *database {
	logger = logger.With(zap.String("component", "database"))

	var db *sqlx.DB
	var err error
	ticker := time.NewTicker(time.Second / 3)
	defer ticker.Stop()
	for {
		db, err = sqlx.ConnectContext(context.TODO(), "mysql", driverConfig().FormatDSN())
		if err == nil {
			break
		}
		logger.Bg().Error("error connecting to db", zap.Error(err))
		<-ticker.C
	}

	d := &database{
		tracer: tracing.InitOTEL("mysql", config.GetOtelExporterType(),
			config.GetMetricsFactory(), logger).Tracer("mysql"),
		logger: logger,
		lock:   &tracing.Mutex{SessionBaggageKey: "request"},
		db:     db,
	}

	d.setupDB()

	return d
}

func driverConfig() *mysql.Config {
	dc := mysql.NewConfig()
	dc.Net = "tcp"
	dc.Addr = config.GetMySQLAddress()
	dc.DBName = config.GetMySQLDatabaseName()
	dc.User = config.GetMySQLUser()
	dc.Passwd = config.GetMySQLPassword()

	dc.Timeout = 60 * time.Second
	dc.InterpolateParams = true
	dc.ParseTime = true
	dc.Params = map[string]string{
		"time_zone": "'+00:00'",
	}
	return dc
}

func (d *database) List(ctx context.Context) ([]Location, error) {
	d.logger.For(ctx).Info("Loading locations", zap.String("location-id", "*"))

	_, span := d.tracer.Start(ctx, "SQL SELECT", trace.WithSpanKind(trace.SpanKindClient))
	span.SetAttributes(
		semconv.PeerServiceKey.String("mysql"),
		attribute.Key("sql.query").String("SELECT id, name, coordinates, zone FROM locations"),
	)
	defer span.End()

	query := "SELECT id, name, coordinates, zone FROM locations"
	rows, err := d.db.Query(query)
	if err != nil {
		if !d.shouldRetry(err) {
			return nil, err
		}
		rows, err = d.db.Query(query)
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()

	var results []Location
	for rows.Next() {
		var id int64
		var name, coordinates string
		var zone sql.NullString

		if err := rows.Scan(&id, &name, &coordinates, &zone); err != nil {
			return nil, err
		}

		results = append(results, Location{
			ID:          id,
			Name:        name,
			Coordinates: coordinates,
			Zone:        nullStringToString(zone),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

func (d *database) Create(ctx context.Context, location *Location) (int64, error) {
	query := "INSERT INTO locations SET name = ?, coordinates = ?, zone = ?"
	res, err := d.db.Exec(query, location.Name, location.Coordinates, nullable(location.Zone))
	if err != nil {
		if !d.shouldRetry(err) {
			return 0, err
		}
		res, err = d.db.Exec(query, location.Name, location.Coordinates, nullable(location.Zone))
		if err != nil {
			return 0, err
		}
	}
	return res.LastInsertId()
}

func (d *database) Update(ctx context.Context, location *Location) error {
	query := "UPDATE locations SET name = ?, coordinates = ?, zone = ? WHERE id = ?"
	res, err := d.db.Exec(query, location.Name, location.Coordinates, nullable(location.Zone), location.ID)
	if err != nil {
		if !d.shouldRetry(err) {
			return err
		}
		res, err = d.db.Exec(query, location.Name, location.Coordinates, nullable(location.Zone), location.ID)
		if err != nil {
			return err
		}
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("wrong number of rows on update: %d != 1", n)
	}
	return nil
}

func (d *database) Get(ctx context.Context, locationID int) (*Location, error) {
	d.logger.For(ctx).Info("Loading location", zap.Int("location_id", locationID))

	_, span := d.tracer.Start(ctx, "SQL SELECT", trace.WithSpanKind(trace.SpanKindClient))
	span.SetAttributes(
		semconv.PeerServiceKey.String("mysql"),
		attribute.Key("sql.query").String(fmt.Sprintf("SELECT id, name, coordinates, zone FROM locations WHERE id = %d", locationID)),
	)
	defer span.End()

	delay.Sleep(config.GetMySQLGetDelay(), config.GetMySQLGetDelayStdDev())

	query := "SELECT id, name, coordinates, zone FROM locations WHERE id = ?"
	row := d.db.QueryRow(query, locationID)

	var id int64
	var name, coordinates string
	var zone sql.NullString

	if err := row.Scan(&id, &name, &coordinates, &zone); err != nil {
		return nil, err
	}

	return &Location{
		ID:          id,
		Name:        name,
		Coordinates: coordinates,
		Zone:        nullStringToString(zone),
	}, nil
}

func (d *database) Delete(ctx context.Context, locationID int) error {
	query := "DELETE FROM locations WHERE id = ?"
	_, err := d.db.Exec(query, locationID)
	if err != nil && !d.shouldRetry(err) {
		return err
	}
	return nil
}

func (d *database) shouldRetry(err error) bool {
	if mysqlErr, ok := err.(*mysql.MySQLError); ok {
		switch mysqlErr.Number {
		case 1146: // Table doesn't exist
			d.setupDB()
			return true
		}
	}
	return false
}

func (d *database) setupDB() {
	fmt.Println("ensuring locations table exists")
	_, err := d.db.Exec(tableSchema)
	if err != nil {
		panic(err)
	}

	fmt.Println("checking if 'zone' column exists")
	var columnName string
	err = d.db.QueryRow(`
		SELECT COLUMN_NAME
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_NAME = 'locations' AND COLUMN_NAME = 'zone'
	`).Scan(&columnName)

	if err == sql.ErrNoRows {
		fmt.Println("'zone' column missing — altering table")
		_, err := d.db.Exec(`ALTER TABLE locations ADD COLUMN zone VARCHAR(255) DEFAULT NULL`)
		if err != nil {
			panic(fmt.Sprintf("failed to alter table: %v", err))
		}
	} else if err != nil {
		panic(fmt.Sprintf("failed to check schema: %v", err))
	} else {
		fmt.Println("'zone' column exists")
	}

	fmt.Println("checking if seed data exists")
	var count int
	err = d.db.QueryRow(`SELECT COUNT(*) FROM locations`).Scan(&count)
	if err != nil {
		panic(fmt.Sprintf("failed to count rows in locations: %v", err))
	}

	if count > 0 {
		fmt.Printf("locations table already has %d rows, skipping seeding\n", count)
		return
	}

	fmt.Println("seeding database")
	stmt, err := d.db.Prepare("INSERT INTO locations (id, name, coordinates, zone) VALUES (?, ?, ?, ?)")
	if err != nil {
		panic(err)
	}
	defer stmt.Close()

	for _, c := range seed {
		if _, err := stmt.Exec(c.ID, c.Name, c.Coordinates, c.Zone); err != nil {
			panic(fmt.Sprintf("error seeding row: %v", err))
		}
	}
}

// helper to convert sql.NullString to plain string
func nullStringToString(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// helper to convert string to interface{} with null support
func nullable(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

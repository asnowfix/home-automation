package storage

import (
	"context"
	"encoding/json"
	"errors"
	"myhome"

	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

type DeviceStorage struct {
	db  *sqlx.DB
	log logr.Logger
}

func NewDeviceStorage(log logr.Logger, dbName string) (*DeviceStorage, error) {
	db, err := sqlx.Connect("sqlite3", dbName)
	if err != nil {
		log.Error(err, "Failed to connect to database", "dbType", "sqlite3", "dbName", dbName)
		return nil, err
	}

	storage := &DeviceStorage{
		db:  db,
		log: log.WithName("DeviceStorage"),
	}
	err = storage.createTable()
	if err != nil {
		log.Error(err, "Failed to create table")
		return nil, err
	}

	return storage, nil
}

func (s *DeviceStorage) createTable() error {
	schema := `
    CREATE TABLE IF NOT EXISTS devices (
        manufacturer TEXT NOT NULL,
        id TEXT NOT NULL,
        mac TEXT UNIQUE,  -- mac can be NULL but must be unique if provided
        name TEXT,
        host TEXT,
        info TEXT,
        config_revision INTEGER,  -- New column for config revision
        config TEXT,
        room_id TEXT DEFAULT '',  -- Room this device belongs to (optional)
        PRIMARY KEY (manufacturer, id)
    );`

	res, err := s.db.Exec(schema)
	if err != nil {
		s.log.Error(err, "Failed to create database schema")
		return err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		s.log.Error(err, "Failed to get rows affected")
		return err
	}
	if rowsAffected > 0 {
		s.log.Info("Created database schema")
	}

	// Drop group tables if they exist (migration from previous version)
	migrationSchema := `
    DROP TABLE IF EXISTS groupsMember;
    DROP TABLE IF EXISTS groups;`

	res, err = s.db.Exec(migrationSchema)
	if err != nil {
		s.log.Error(err, "Failed to drop group tables during migration")
		// Don't return error - tables might not exist
	}
	rowsAffected, err = res.RowsAffected()
	if err != nil {
		s.log.Error(err, "Failed to get rows affected")
		// Don't return error - tables might not exist
	}
	if rowsAffected > 0 {
		s.log.Info("Dropped group tables during migration")
	}

	// Migration: Add room_id column if it doesn't exist
	var count int
	query := `SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name='room_id'`
	err = s.db.Get(&count, query)
	if err != nil {
		s.log.Error(err, "Failed to check for room_id column")
		return err
	}
	if count == 0 {
		s.log.Info("Adding room_id column to devices table")
		alterQuery := `ALTER TABLE devices ADD COLUMN room_id TEXT DEFAULT ''`
		res, err = s.db.Exec(alterQuery)
		if err != nil {
			s.log.Error(err, "Failed to add room_id column")
			return err
		}
		rowsAffected, err = res.RowsAffected()
		if err != nil {
			s.log.Error(err, "Failed to get rows affected")
			// Don't return error - tables might not exist
		}
	}

	return nil
}

// DB returns the underlying database connection for use by other services
func (s *DeviceStorage) DB() *sqlx.DB {
	return s.db
}

// Close closes the database connection & syncs it to persistent storage.
func (s *DeviceStorage) Close() {
	s.log.Info("Closing database connection")
	s.db.Close()
}

func (s *DeviceStorage) Flush() error {
	return nil // TODO empty database
}

// UpsertDevice update a device into the database, creating it on the fly if necessary
func (s *DeviceStorage) SetDevice(ctx context.Context, device *myhome.Device, overwrite bool) (bool, error) {
	d := Device{
		Device: *device,
	}
	b, err := json.Marshal(device.Info)
	if err != nil {
		s.log.Error(err, "Failed to marshal device info", "device", device)
		return false, err
	}
	d.Info_ = string(b)
	s.log.V(1).Info("Marshalled device info", "device_id", device.Id(), "info_length", len(d.Info_), "info_is_null", device.Info == nil)

	b, err = json.Marshal(device.Config)
	if err != nil {
		s.log.Error(err, "Failed to marshal device config", "device", device)
		return false, err
	}
	d.Config_ = string(b)
	s.log.V(1).Info("Marshalled device config", "device_id", device.Id(), "config_length", len(d.Config_), "config_is_null", device.Config == nil)

	// Number of rows affected by the SQL
	var count int64

	// Use INSERT ... ON CONFLICT with WHERE clause to only update when values actually differ
	// This is much more efficient than SELECT-then-UPDATE pattern
	query := `
    INSERT INTO devices (manufacturer, id, mac, name, host, info, config_revision, config, room_id) 
    VALUES (:manufacturer, :id, :mac, :name, :host, :info, :config_revision, :config, :room_id)
    ON CONFLICT(manufacturer, id) DO UPDATE SET 
        mac = excluded.mac, 
        name = excluded.name, 
        host = excluded.host, 
        info = excluded.info, 
        config_revision = excluded.config_revision, 
        config = excluded.config,
        room_id = excluded.room_id
    WHERE devices.mac IS NOT excluded.mac
       OR devices.name IS NOT excluded.name
       OR devices.host IS NOT excluded.host
       OR devices.info IS NOT excluded.info
       OR devices.config_revision IS NOT excluded.config_revision
       OR devices.config IS NOT excluded.config
       OR devices.room_id IS NOT excluded.room_id`
	rows, err := s.db.NamedExec(query, d)
	if err != nil {
		s.log.Error(err, "Failed to upsert device by manufacturer and id", "device", device)
		return false, err
	}
	count, err = rows.RowsAffected()
	if err != nil {
		s.log.Error(err, "Failed to upsert device by manufacturer and id", "device", device)
		return false, err
	}
	if count > 0 {
		return true, nil
	}

	// If MAC address is provided, also handle conflicts based on MAC address
	if d.MAC != "" {
		macQuery := `
    UPDATE devices SET 
        manufacturer = :manufacturer,
        id = :id,
        name = :name, 
        host = :host, 
        info = :info, 
        config_revision = :config_revision, 
        config = :config
    WHERE mac = :mac`

		rows, err := s.db.NamedExec(macQuery, d)
		if err != nil {
			s.log.Error(err, "Failed to update device by MAC address", "device", device)
			return false, err
		}
		count, err = rows.RowsAffected()
		if err != nil {
			s.log.Error(err, "Failed to update device by MAC address", "device", device)
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}

	return false, nil
}

// GetAllDevices retrieves all devices from the database.
func (s *DeviceStorage) GetAllDevices(ctx context.Context) ([]*myhome.Device, error) {
	devices := make([]Device, 0)
	query := `SELECT * FROM devices`
	err := s.db.Select(&devices, query)
	if err != nil {
		s.log.Error(err, "Failed to get all devices")
		return nil, err
	}
	return unmarshallDevices(s.log, devices)
}

// GetDeviceByAny retrieves a device from the database by one of its identifiers (Id, MAC address, name, host)
func (s *DeviceStorage) GetDeviceByAny(ctx context.Context, any string) (*myhome.Device, error) {
	var device Device
	query := `SELECT * FROM devices WHERE name = $1 OR id = $1 OR mac = $1 OR host = $1`
	err := s.db.Get(&device, query, any)
	if err != nil {
		s.log.Error(err, "Failed to get device by identifier", "identifier", any)
		return nil, err
	}
	s.log.Info("Got device by identifier", "identifier", any, "device", device)
	return unmarshallDevice(s.log, device)
}

// GetDeviceByManufacturerAndID retrieves a device from the database by its manufacturer and ID.
func (s *DeviceStorage) GetDeviceById(ctx context.Context, id string) (*myhome.Device, error) {
	var device Device
	query := `SELECT * FROM devices WHERE id = $1`
	err := s.db.Get(&device, query, id)
	if err != nil {
		s.log.Error(err, "Failed to get device by Id", "id", id)
		return nil, err
	}
	s.log.Info("Got device by Id", "id", id, "device", device)
	return unmarshallDevice(s.log, device)
}

// GetDeviceByMAC retrieves a device from the database by its MAC address.
func (s *DeviceStorage) GetDeviceByMAC(ctx context.Context, mac string) (*myhome.Device, error) {
	if len(mac) == 0 {
		return nil, errors.New("empty mac")
	}
	var device Device
	query := `SELECT * FROM devices WHERE mac = $1`
	err := s.db.Get(&device, query, mac)
	if err != nil {
		s.log.Error(err, "Failed to get device by MAC", "mac", mac)
		return nil, err
	}
	return unmarshallDevice(s.log, device)
}

// GetDeviceByName retrieves a device from the database by its name.
func (s *DeviceStorage) GetDeviceByName(ctx context.Context, name string) (*myhome.Device, error) {
	var device Device
	query := `SELECT * FROM devices WHERE name = $1`
	err := s.db.Get(&device, query, name)
	if err != nil {
		s.log.Error(err, "Failed to get device by name", "name", name)
		return nil, err
	}
	return unmarshallDevice(s.log, device)
}

func (s *DeviceStorage) GetDevicesMatchingAny(ctx context.Context, name string) ([]*myhome.Device, error) {
	devices := make([]Device, 0)
	query := `SELECT * FROM devices WHERE name LIKE '%' || $1 || '%' OR id LIKE '%' || $1 || '%' OR mac LIKE '%' || $1 || '%' OR host LIKE '%' || $1 || '%'`
	err := s.db.Select(&devices, query, name)
	if err != nil {
		s.log.Error(err, "Failed to get all devices")
		return nil, err
	}
	return unmarshallDevices(s.log, devices)
}

func (s *DeviceStorage) GetDeviceByHost(ctx context.Context, host string) (*myhome.Device, error) {
	var device Device
	query := `SELECT * FROM devices WHERE host = $1`
	err := s.db.Get(&device, query, host)
	if err != nil {
		s.log.Error(err, "Failed to get device by host", "host", host)
		return nil, err
	}
	return unmarshallDevice(s.log, device)
}

// ForgetDevice deletes a device from the database by any of its identifiers (Id, MAC address, name, host)
func (s *DeviceStorage) ForgetDevice(ctx context.Context, identifier string) error {
	query := `DELETE FROM devices WHERE id = $1 OR mac = $1 OR name = $1 OR host = $1`
	res, err := s.db.Exec(query, identifier)
	if err != nil {
		s.log.Error(err, "Failed to delete device", "identifier", identifier)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		s.log.Error(err, "Failed to get rows affected")
	} else {
		s.log.Info("Device deleted", "identifier", identifier, "nb_deleted", rowsAffected)
	}
	return err
}

// GetDevicesByRoom retrieves all devices in a specific room
func (s *DeviceStorage) GetDevicesByRoom(ctx context.Context, roomId string) ([]*myhome.Device, error) {
	devices := make([]Device, 0)
	query := `SELECT * FROM devices WHERE room_id = $1`
	err := s.db.Select(&devices, query, roomId)
	if err != nil {
		s.log.Error(err, "Failed to get devices by room", "room_id", roomId)
		return nil, err
	}
	return unmarshallDevices(s.log, devices)
}

// SetDeviceRoom updates the room assignment for a device
func (s *DeviceStorage) SetDeviceRoom(ctx context.Context, identifier string, roomId string) (bool, error) {
	query := `UPDATE devices SET room_id = $1 WHERE id = $2 OR mac = $2 OR name = $2 OR host = $2`
	res, err := s.db.Exec(query, roomId, identifier)
	if err != nil {
		s.log.Error(err, "Failed to set device room", "identifier", identifier, "room_id", roomId)
		return false, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		s.log.Error(err, "Failed to set device room", "identifier", identifier, "room_id", roomId)
		return false, err
	}
	return rowsAffected > 0, nil
}

// unmarshallDevice takes a Device struct and unmarshals the Info and Config fields
func unmarshallDevice(log logr.Logger, device Device) (*myhome.Device, error) {
	err := json.Unmarshal([]byte(device.Info_), &device.Info)
	if err != nil {
		log.Error(err, "Failed to unmarshal storage info", "device_id", device.Id, "info", device.Info_)
		// return myhome.Device{}, err
	}
	err = json.Unmarshal([]byte(device.Config_), &device.Config)
	if err != nil {
		log.Error(err, "Failed to unmarshal storage config", "device_id", device.Id, "config", device.Config_)
		// return myhome.Device{}, err
	}
	return &device.Device, nil
}

// unmarshallDevices takes a slice of Device structs and unmarshals the Info and Config fields
func unmarshallDevices(log logr.Logger, ds []Device) ([]*myhome.Device, error) {
	mhd := make([]*myhome.Device, 0)
	for _, device := range ds {
		d, err := unmarshallDevice(log, device)
		if err != nil {
			log.Error(err, "Failed to unmarshall storage", "device_id", device.Id)
			return mhd, err
		}
		mhd = append(mhd, d)
	}
	return mhd, nil
}

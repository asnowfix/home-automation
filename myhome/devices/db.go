package devices

import (
	"errors"

	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3" // or any other SQL driver
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
        config TEXT,
        status TEXT,
        PRIMARY KEY (manufacturer, id)
    );`
	_, err := s.db.Exec(schema)
	if err != nil {
		s.log.Error(err, "Failed to execute create table query")
	}
	s.log.Info("Created table")
	return err
}

func (s *DeviceStorage) Close() {
	s.log.Info("Closing database connection")
	s.db.Close()
}

func (s *DeviceStorage) UpsertDevice(device *Device) error {
	query := `
    INSERT INTO devices (manufacturer, id, mac, name, host, info, config, status) 
    VALUES (:manufacturer, :id, :mac, :name, :host, :info, :config, :status)
    ON CONFLICT(manufacturer, id) DO UPDATE SET 
        mac = excluded.mac, 
        name = excluded.name, 
        host = excluded.host, 
        info = excluded.info, 
        config = excluded.config, 
        status = excluded.status`
	_, err := s.db.NamedExec(query, device)
	if err != nil {
		s.log.Error(err, "Failed to upsert device", "device", device)
		return err
	}
	return nil
}

func (s *DeviceStorage) GetDeviceByIdentifier(identifier string) (*Device, error) {
	var device Device
	query := `SELECT * FROM devices WHERE id = $1 OR mac = $1 OR name = $1 OR host = $1`
	err := s.db.Get(&device, query, identifier)
	if err != nil {
		s.log.Error(err, "Failed to get device by identifier", "identifier", identifier)
		return nil, err
	}
	s.log.Info("Got device by identifier", "identifier", identifier, "device", device)
	return &device, nil
}

func (s *DeviceStorage) GetDeviceByManufacturerAndID(manufacturer, id string) (*Device, error) {
	var device Device
	query := `SELECT * FROM devices WHERE manufacturer = $1 AND id = $2`
	err := s.db.Get(&device, query, manufacturer, id)
	if err != nil {
		s.log.Error(err, "Failed to get device by manufacturer and ID", "manufacturer", manufacturer, "id", id)
		return nil, err
	}
	s.log.Info("Got device by manufacturer and ID", "manufacturer", manufacturer, "id", id, "device", device)
	return &device, nil
}

func (s *DeviceStorage) GetDeviceByMAC(mac string) (*Device, error) {
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
	return &device, nil
}

func (s *DeviceStorage) GetDeviceByName(name string) (*Device, error) {
	var device Device
	query := `SELECT * FROM devices WHERE name = $1`
	err := s.db.Get(&device, query, name)
	if err != nil {
		s.log.Error(err, "Failed to get device by name", "name", name)
		return nil, err
	}
	return &device, nil
}

func (s *DeviceStorage) GetAllDevices() ([]Device, error) {
	var devices []Device
	query := `SELECT * FROM devices`
	err := s.db.Select(&devices, query)
	if err != nil {
		s.log.Error(err, "Failed to get all devices")
		return nil, err
	}
	return devices, nil
}

func (s *DeviceStorage) DeleteDevice(mac string) error {
	query := `DELETE FROM devices WHERE mac = $1`
	_, err := s.db.Exec(query, mac)
	if err != nil {
		s.log.Error(err, "Failed to delete device", "mac", mac)
	}
	return err
}

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
        config_revision INTEGER,  -- New column for config revision
        config TEXT,
        status TEXT,
        PRIMARY KEY (manufacturer, id)
    );

    CREATE TABLE IF NOT EXISTS groups (
        id SERIAL PRIMARY KEY,
        name TEXT UNIQUE NOT NULL,
        description TEXT
    );

    CREATE TABLE IF NOT EXISTS groupsMember (
        manufacturer TEXT NOT NULL,
        id TEXT NOT NULL,
        group_id INTEGER NOT NULL,
        UNIQUE(manufacturer, id, group_id),
        FOREIGN KEY (manufacturer, id) REFERENCES devices(manufacturer, id) ON DELETE CASCADE,
        FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE CASCADE
    );
`
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
    INSERT INTO devices (manufacturer, id, mac, name, host, info, config_revision, config, status) 
    VALUES (:manufacturer, :id, :mac, :name, :host, :info, :config_revision, :config, :status)
    ON CONFLICT(manufacturer, id) DO UPDATE SET 
        mac = excluded.mac, 
        name = excluded.name, 
        host = excluded.host, 
        info = excluded.info, 
        config_revision = excluded.config_revision, 
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

type Group struct {
	ID          int    `db:"id"`
	Name        string `db:"name"`
	Description string `db:"description"`
}

func (s *DeviceStorage) GetAllGroups() ([]Group, error) {
	s.log.Info("Retrieving all groups")
	groups := make([]Group, 0)
	query := "SELECT id, name, description FROM groups"
	err := s.db.Select(&groups, query)
	if err != nil {
		s.log.Error(err, "Failed to retrieve groups")
		return nil, err
	}
	return groups, nil
}

func (s *DeviceStorage) GetDevicesByGroupName(name string) ([]Device, error) {
	log := s.log.WithValues("group", name)
	log.Info("Retrieving devices for group")
	devices := make([]Device, 0)

	// First, get the group ID for the given group name
	var groupID int
	err := s.db.Get(&groupID, "SELECT id FROM groups WHERE name = $1", name)
	if err != nil {
		log.Error(err, "Did not find a group with", "name", name)
		return nil, err
	}

	// Now, query devices that have this group ID
	query := "SELECT d.* FROM devices d INNER JOIN groupsMember gm ON d.manufacturer = gm.manufacturer AND d.id = gm.id WHERE gm.group_id = $1"
	err = s.db.Select(&devices, query, groupID)
	if err != nil {
		log.Error(err, "Failed to retrieve devices for group", "name", name)
		return nil, err
	}

	return devices, nil
}

// AddGroup adds a new group to the database.
func (s *DeviceStorage) AddGroup(name, description string) error {
	log := s.log.WithValues("name", name)
	log.Info("Adding new group")
	query := `INSERT INTO groups (name, description) VALUES (:name, :description)`
	_, err := s.db.NamedExec(query, map[string]interface{}{
		"name":        name,
		"description": description,
	})
	return err
}

// RemoveGroup removes a group from the database.
func (s *DeviceStorage) RemoveGroup(groupID int) error {
	log := s.log.WithValues("groupID", groupID)
	log.Info("Removing group")
	query := `DELETE FROM groups WHERE id = $1`
	_, err := s.db.Exec(query, groupID)
	return err
}

// AddDeviceToGroup adds a device to a group.
func (s *DeviceStorage) AddDeviceToGroup(manufacturer, id string, groupID int) error {
	query := `INSERT INTO groupsMember (manufacturer, id, group_id) VALUES ($1, $2, $3)`
	_, err := s.db.Exec(query, manufacturer, id, groupID)
	return err
}

// RemoveDeviceFromGroup removes a device from a group.
func (s *DeviceStorage) RemoveDeviceFromGroup(manufacturer, id string, groupID int) error {
	query := `DELETE FROM groupsMember WHERE manufacturer = $1 AND id = $2 AND group_id = $3`
	_, err := s.db.Exec(query, manufacturer, id, groupID)
	return err
}

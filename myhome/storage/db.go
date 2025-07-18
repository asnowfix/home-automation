package storage

import (
	"context"
	"database/sql"
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
        PRIMARY KEY (manufacturer, id)
    );

    CREATE TABLE IF NOT EXISTS groups (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT UNIQUE NOT NULL,
        kvs TEXT
    );

    CREATE TABLE IF NOT EXISTS groupsMember (
        manufacturer TEXT NOT NULL,
        id TEXT NOT NULL,
        group_id INTEGER NOT NULL,
        FOREIGN KEY (manufacturer, id) REFERENCES devices(manufacturer, id) ON DELETE CASCADE,
        FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE RESTRICT,
        PRIMARY KEY (manufacturer, id, group_id)
    );`

	_, err := s.db.Exec(schema)
	if err != nil {
		s.log.Error(err, "Failed to create database schema")
		return err
	}
	return nil
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
func (s *DeviceStorage) SetDevice(ctx context.Context, device *myhome.Device, overwrite bool) error {
	d := Device{
		Device: *device,
	}
	b, err := json.Marshal(device.Info)
	if err != nil {
		s.log.Error(err, "Failed to marshal device info", "device", device)
		return err
	}
	d.Info_ = string(b)
	b, err = json.Marshal(device.Config)
	if err != nil {
		s.log.Error(err, "Failed to marshal device config", "device", device)
		return err
	}
	d.Config_ = string(b)

	// First, try to insert or update based on manufacturer and id
	query := `
    INSERT INTO devices (manufacturer, id, mac, name, host, info, config_revision, config) 
    VALUES (:manufacturer, :id, :mac, :name, :host, :info, :config_revision, :config)
    ON CONFLICT(manufacturer, id) DO UPDATE SET 
        mac = excluded.mac, 
        name = excluded.name, 
        host = excluded.host, 
        info = excluded.info, 
        config_revision = excluded.config_revision, 
        config = excluded.config`
	_, err = s.db.NamedExec(query, d)
	if err != nil {
		s.log.Error(err, "Failed to upsert device by manufacturer and id", "device", device)
		return err
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

		_, err = s.db.NamedExec(macQuery, d)
		if err != nil {
			s.log.Error(err, "Failed to update device by MAC address", "device", device)
			return err
		}
	}

	return nil
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
	_, err := s.db.Exec(query, identifier)
	if err != nil {
		s.log.Error(err, "Failed to delete device", "identifier", identifier)
	}
	s.log.Info("Device deleted", "identifier", identifier)
	return err
}

// GetAllGroups retrieves all groups from the database.
func (s *DeviceStorage) GetAllGroups() (*myhome.Groups, error) {
	s.log.Info("Retrieving all groups")
	var groups myhome.Groups
	query := "SELECT id, name, kvs FROM groups"
	err := s.db.Select(&groups.Groups, query)
	if err != nil {
		s.log.Error(err, "Failed to retrieve groups")
		return nil, err
	}
	return &groups, nil
}

// GetGroupInfo retrieves information about a specific group.
func (s *DeviceStorage) GetGroupInfo(name string) (*myhome.GroupInfo, error) {
	log := s.log.WithValues("group", name)
	log.Info("Retrieving group info")
	var gi myhome.GroupInfo

	query := "SELECT id, name, kvs FROM groups WHERE name = $1"
	err := s.db.Get(&gi, query, name)
	if err != nil {
		log.Error(err, "Failed to get group info")
		return nil, err
	}

	return &gi, nil
}

// GetDevicesByGroupName retrieves the devices for a specific group.
func (s *DeviceStorage) GetDevicesByGroupName(name string) ([]*myhome.Device, error) {
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

	return unmarshallDevices(log, devices)
}

// GetDeviceGroups retrieves the groups for a specific device.
func (s *DeviceStorage) GetDeviceGroups(manufacturer, id string) (*myhome.Groups, error) {
	var groups myhome.Groups
	query := "SELECT g.* FROM groups g INNER JOIN groupsMember gm ON g.id = gm.group_id WHERE gm.manufacturer = $1 AND gm.id = $2"
	err := s.db.Select(&groups.Groups, query, manufacturer, id)
	if err != nil {
		s.log.Error(err, "Failed to retrieve groups for device", "manufacturer", manufacturer, "id", id)
		return nil, err
	}
	return &groups, nil
}

// AddGroup adds a new group to the database.
func (s *DeviceStorage) AddGroup(group *myhome.GroupInfo) (any, error) {

	// Check if the group already exists, if so, update it
	var exists bool
	var result sql.Result
	var id int64
	var err error

	err = s.db.Get(&exists, "SELECT EXISTS(SELECT 1 FROM groups WHERE name = $1)", group.Name)
	if err != nil {
		s.log.Error(err, "Failed to check if group exists", "name", group.Name)
		return nil, err
	}

	if exists {
		s.log.Info("Group already exists, updating", "name", group.Name)
		query := `UPDATE groups SET kvs = :kvs WHERE name = :name`
		result, err = s.db.NamedExec(query, map[string]interface{}{
			"name": group.Name,
			"kvs":  group.KVS,
		})
	} else {
		log := s.log.WithValues("name", group.Name)
		log.Info("Adding new group", "name", group.Name)
		query := `INSERT INTO groups (name, kvs) VALUES (:name, :kvs)`
		result, err = s.db.NamedExec(query, map[string]interface{}{
			"name": group.Name,
			"kvs":  group.KVS,
		})
	}

	if err != nil {
		return nil, err
	}
	id, err = result.LastInsertId()
	if err != nil {
		return nil, err
	}
	group.ID = int(id)
	return group, nil
}

// RemoveGroup removes a group from the database by its name.
// RemoveGroup removes a group from the database by its name.
func (s *DeviceStorage) RemoveGroup(name string) (any, error) {
	log := s.log.WithValues("name", name)
	log.Info("Removing group", "name", name)

	// Check if the group exists
	var exists bool
	err := s.db.Get(&exists, "SELECT EXISTS(SELECT 1 FROM groups WHERE name = $1)", name)
	if err != nil {
		log.Error(err, "Failed to check if group exists", "name", name)
		return nil, err
	}
	if !exists {
		log.Info("Group does not exist", "name", name)
		return nil, errors.New("group does not exist")
	}

	// Proceed to delete the group
	query := `DELETE FROM groups WHERE name = :name`
	_, err = s.db.NamedExec(query, map[string]interface{}{
		"name": name,
	})
	return nil, err
}

// AddDeviceToGroup adds a device to a group.
func (s *DeviceStorage) AddDeviceToGroup(groupDevice *myhome.GroupDevice) (any, error) {
	query := `INSERT INTO groupsMember (manufacturer, id, group_id) VALUES (:manufacturer, :id, (SELECT id FROM groups WHERE name = :group))`
	_, err := s.db.NamedExec(query, groupDevice)
	return nil, err
}

// RemoveDeviceFromGroup removes a device from a group.
func (s *DeviceStorage) RemoveDeviceFromGroup(groupDevice *myhome.GroupDevice) (any, error) {
	query := `DELETE FROM groupsMember WHERE manufacturer = $1 AND id = $2 AND group_id = (SELECT id FROM groups WHERE name = $3)`
	_, err := s.db.Exec(query, groupDevice.Manufacturer, groupDevice.Id, groupDevice.Group)
	return nil, err
}

// unmarshallDevice takes a Device struct and unmarshals the Info, Config, and Status fields
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

// unmarshallDevices takes a slice of Device structs and unmarshals the Info, Config, and Status fields
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

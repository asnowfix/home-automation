package vacuum

import (
	"fmt"
	"strings"

	"github.com/asnowfix/home-automation/internal/myhome"
)

// dumpTables are the tables `ctl db export` carries, parents first. volatile
// columns are bookkeeping timestamps that are re-set on import, not copied.
var dumpTables = []struct {
	name     string
	order    string
	volatile []string
}{
	{"vacuum_robots", "blid", []string{"updated_at"}},
	{"vacuum_regions", "blid, pmap_id, region_id", []string{"synced_at"}},
}

// Export returns every robot and region row, column by column. It reads
// `SELECT *`, so a column added by a later migration (vacuum_regions.room_id,
// #684) is exported without a change here.
func (s *Storage) Export() (*myhome.VacuumDump, error) {
	dump := &myhome.VacuumDump{}
	for _, t := range dumpTables {
		rows, err := s.db.Queryx(`SELECT * FROM ` + t.name + ` ORDER BY ` + t.order)
		if err != nil {
			return nil, fmt.Errorf("failed to export %s: %w", t.name, err)
		}
		var out []myhome.VacuumRow
		for rows.Next() {
			row := map[string]any{}
			if err := rows.MapScan(row); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("failed to read %s: %w", t.name, err)
			}
			for _, c := range t.volatile {
				delete(row, c)
			}
			for k, v := range row {
				if b, ok := v.([]byte); ok {
					row[k] = string(b)
				}
			}
			out = append(out, row)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
		if t.name == "vacuum_robots" {
			dump.Robots = out
		} else {
			dump.Regions = out
		}
	}
	return dump, nil
}

// Import stores the dump in one transaction. A row replaces the row with the
// same primary key. Keys that are not columns of this build's table are
// ignored, and columns the row lacks take their default, so dumps move between
// builds whose schemas differ.
func (s *Storage) Import(dump *myhome.VacuumDump) (*myhome.VacuumImportResult, error) {
	tx, err := s.db.Beginx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	res := &myhome.VacuumImportResult{}
	for _, t := range dumpTables {
		rows := dump.Robots
		if t.name == "vacuum_regions" {
			rows = dump.Regions
		}
		if len(rows) == 0 {
			continue
		}
		var cols []string
		if err := tx.Select(&cols, `SELECT name FROM pragma_table_info('`+t.name+`')`); err != nil {
			return nil, err
		}
		known := map[string]bool{}
		for _, c := range cols {
			known[c] = true
		}
		for _, v := range t.volatile {
			delete(known, v)
		}
		for _, row := range rows {
			var names []string
			var args []any
			for k, v := range row {
				if known[k] && v != nil { // NULL: let the column default apply
					names = append(names, k)
					args = append(args, v)
				}
			}
			if len(names) == 0 {
				continue
			}
			q := `INSERT OR REPLACE INTO ` + t.name + ` (` + strings.Join(names, ",") + `) VALUES (?` + strings.Repeat(",?", len(names)-1) + `)`
			if _, err := tx.Exec(q, args...); err != nil {
				return nil, fmt.Errorf("failed to import into %s: %w", t.name, err)
			}
			if t.name == "vacuum_robots" {
				res.Robots++
			} else {
				res.Regions++
			}
		}
	}
	return res, tx.Commit()
}

package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var psql *gorm.DB

func initPSql(host, port, user, password, db string) error {

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s", host, user, password, db, port)
	var err error
	if psql, err = gorm.Open(postgres.Open(dsn)); err != nil {
		return err
	}
	return nil
}

type Drive struct {
	ID              int64 `gorm:"column:id"`
	StartPositionID int64 `gorm:"column:start_position_id"`
	EndPositionID   int64 `gorm:"column:end_position_id "`
	StartAddressID  int64 `gorm:"column:start_address_id"`
	EndAddressID    int64 `gorm:"column:end_address_id"`
}

type Position struct {
	ID        int64   `gorm:"column:id"`
	Latitude  float64 `gorm:"column:latitude"`
	Longitude float64 `gorm:"column:longitude"`
}

type Address struct {
	ID            int64          `gorm:"column:id"`
	DisplayName   string         `gorm:"column:display_name"`
	Latitude      float64        `gorm:"column:latitude"`
	Longitude     float64        `gorm:"column:longitude"`
	Name          string         `gorm:"column:name"`
	HouseNumber   sql.NullString `gorm:"column:house_number"`
	Road          sql.NullString `gorm:"column:road"`
	Neighbourhood sql.NullString `gorm:"column:neighbourhood"`
	City          sql.NullString `gorm:"column:city"`
	County        sql.NullString `gorm:"column:county"`
	Postcode      sql.NullString `gorm:"column:postcode"`
	State         sql.NullString `gorm:"column:state"`
	StateDistrict sql.NullString `gorm:"column:state_district"`
	Country       sql.NullString `gorm:"column:country"`
	Raw           []byte         `gorm:"column:raw"`
	InsertedAt    int64          `gorm:"column:inserted_at"`
	UpdatedAt     int64          `gorm:"column:updated_at"`
	OsmID         int64          `gorm:"column:osm_id"`
	OsmType       string         `gorm:"column:osm_type"`
}

func saveBrokenAddr() error {
	return psql.Transaction(func(tx *gorm.DB) error {
		var drives []*Drive
		err := tx.Table("drives").Where("start_address_id IS NULL").Or("end_address_id IS NULL").Find(&drives).Error
		if err != nil {
			return err
		}

		positions := []*Position{}
		for _, d := range drives {
			startPos, endPos := &Position{}, &Position{}
			tx.Table("positions").Where("id = ?", d.StartPositionID).First(startPos)
			tx.Table("positions").Where("id = ?", d.EndPositionID).First(endPos)
			positions = append(positions, startPos, endPos)
		}

		for _, p := range positions {
			addr := &Address{}
			tx.Table("addresses").Where("latitude = ?", p.Latitude).Where("longitude = ?", p.Longitude).First(addr)

			if addr.ID > 0 {
				continue
			}
			osmAddr, err := getAddressByProxy(p.Latitude, p.Longitude)
			if err != nil {
				log.Printf("get address from osm failed, lat=%v, lon=%v, err=%#v", p.Latitude, p.Longitude, err)
				continue
			}

			dnames := strings.Split(osmAddr.DisplayName, ",")
			var name string
			if len(dnames) > 0 {
				name = strings.TrimSpace(dnames[0])
			}
			raw, _ := json.Marshal(osmAddr.Address)
			newAddr := &Address{
				DisplayName:   osmAddr.DisplayName,
				Latitude:      p.Latitude,
				Longitude:     p.Longitude,
				Name:          name,
				HouseNumber:   getOrNull(osmAddr.Address, "housenumber"),
				Road:          getOrNull(osmAddr.Address, "road"),
				Neighbourhood: getOrNull(osmAddr.Address, "neighbourhood"),
				City:          getOrNull(osmAddr.Address, "city"),
				County:        getOrNull(osmAddr.Address, "county"),
				Postcode:      getOrNull(osmAddr.Address, "postcode"),
				State:         getOrNull(osmAddr.Address, "state"),
				StateDistrict: getOrNull(osmAddr.Address, "state_district"),
				Country:       getOrNull(osmAddr.Address, "country"),
				Raw:           raw,
				InsertedAt:    time.Now().Unix(),
				UpdatedAt:     time.Now().Unix(),
				OsmID:         int64(osmAddr.OsmID),
				OsmType:       osmAddr.OsmType,
			}

			err = tx.Table("addresses").Create(newAddr).Error
			if err == nil {
				log.Printf("save address success, addr=%+v", newAddr)
			}
		}
		return nil
	})
}

func getOrNull(m map[string]interface{}, key string) sql.NullString {
	v, ok := m[key]
	if !ok {
		return sql.NullString{}
	}
	cv, ok := v.(string)
	if !ok {
		return sql.NullString{}
	}
	return sql.NullString{
		String: cv,
		Valid:  true,
	}
}

func fixAddrBroken() error {
	return psql.Transaction(func(tx *gorm.DB) error {
		var drives []*Drive
		err := tx.Table("drives").Where("start_address_id IS NULL").Or("end_address_id IS NULL").Find(&drives).Error
		if err != nil {
			return err
		}
		for _, d := range drives {
			startPos, endPos := &Position{}, &Position{}
			tx.Table("positions").Where("id = ?", d.StartPositionID).First(startPos)
			tx.Table("positions").Where("id = ?", d.EndPositionID).First(endPos)

			startAddr, endAddr := &Address{}, &Address{}
			tx.Table("addresses").Where("latitude = ?", startPos.Latitude).Where("longitude = ?", startPos.Longitude).First(startAddr)
			if startAddr.ID > 0 {
				err := tx.Table("drives").Where("id = ?", d.ID).Update("start_address_id", startAddr.ID)
				if err == nil {
					log.Printf("fix address success, drives id=%v, fix start addr=%v", d.ID, startAddr.DisplayName)
				}
			}

			tx.Table("addresses").Where("latitude = ?", endPos.Latitude).Where("longitude = ?", endPos.Longitude).First(endAddr)
			if endAddr.ID > 0 {
				tx.Table("drives").Where("id = ?", d.ID).Update("end_address_id", endAddr.ID)
				if err == nil {
					log.Printf("fix address success, drives id=%v, fix end addr=%v", d.ID, endAddr.DisplayName)
				}
			}
		}
		return nil
	})
}

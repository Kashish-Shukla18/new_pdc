package store

import (
	"context"
	"fmt"
	"os"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"pdc/config"
)

type Store struct {
	client influxdb2.Client
	query  api.QueryAPI
	write  api.WriteAPIBlocking
	org    string
	bucket string
}

func env(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func NewStore() (*Store, error) {
	url := env("INFLUX_URL", "http://127.0.0.1:8087")
	token := env("INFLUX_TOKEN", "my-super-secret-token")
	org := env("INFLUX_ORG", "pdc-org")
	bucket := env("INFLUX_BUCKET", "synchrophasor")

	client := influxdb2.NewClient(url, token)
	if ok, err := client.Health(context.Background()); err != nil {
		client.Close()
		return nil, fmt.Errorf("influx health failed: %w", err)
	} else if ok.Status != "pass" {
		client.Close()
		return nil, fmt.Errorf("influx not healthy: %s", ok.Status)
	}

	return &Store{
		client: client,
		query:  client.QueryAPI(org),
		write:  client.WriteAPIBlocking(org, bucket),
		org:    org,
		bucket: bucket,
	}, nil
}

func (s *Store) Close() {
	s.client.Close()
}

func (s *Store) SavePMU(ctx context.Context, cfg config.PMUConfig) error {
	p := influxdb2.NewPoint(
		"pmu_config",
		map[string]string{"name": cfg.Name},
		map[string]interface{}{
			"ip":            cfg.IP,
			"port":          cfg.Port,
			"tcp_port":      cfg.TCPPort,
			"idcode":        int(cfg.IDCode),
			"protocol":      cfg.Protocol,
			"timeout_sec":   cfg.TimeoutSec,
			"reconnect_sec": cfg.ReconnectSec,
			"region":        cfg.Region,
			"lat":           cfg.Lat,
			"lon":           cfg.Lon,
			"active":        1,
		},
		time.Now(),
	)
	return s.write.WritePoint(ctx, p)
}

func (s *Store) DeletePMU(ctx context.Context, name string) error {
	p := influxdb2.NewPoint(
		"pmu_config",
		map[string]string{"name": name},
		map[string]interface{}{
			"active": 0,
		},
		time.Now(),
	)
	return s.write.WritePoint(ctx, p)
}

func (s *Store) GetAllPMUs(ctx context.Context) ([]config.PMUConfig, error) {
	flux := fmt.Sprintf(`
		from(bucket:"%s")
		  |> range(start: 0)
		  |> filter(fn: (r) => r._measurement == "pmu_config")
		  |> pivot(rowKey:["_time", "name"], columnKey: ["_field"], valueColumn: "_value")
		  |> group(columns: ["name"])
		  |> sort(columns: ["_time"], desc: true)
		  |> limit(n: 1)
	`, s.bucket)

	result, err := s.query.Query(ctx, flux)
	if err != nil {
		return nil, err
	}
	defer result.Close()

	var pmus []config.PMUConfig
	for result.Next() {
		rec := result.Record()
		
		activeVal := rec.ValueByKey("active")
		if activeVal == nil {
			continue
		}
		var active int64
		switch v := activeVal.(type) {
		case int64:
			active = v
		case float64:
			active = int64(v)
		}

		if active == 0 {
			continue // deleted
		}

		name, _ := rec.ValueByKey("name").(string)
		ip, _ := rec.ValueByKey("ip").(string)
		protocol, _ := rec.ValueByKey("protocol").(string)
		region, _ := rec.ValueByKey("region").(string)

		var port, idcode, timeout, reconnect, tcpPort int64
		var lat, lon float64

		if v := rec.ValueByKey("port"); v != nil {
			switch val := v.(type) {
			case int64: port = val
			case float64: port = int64(val)
			}
		}
		if v := rec.ValueByKey("tcp_port"); v != nil {
			switch val := v.(type) {
			case int64: tcpPort = val
			case float64: tcpPort = int64(val)
			}
		}
		if v := rec.ValueByKey("idcode"); v != nil {
			switch val := v.(type) {
			case int64: idcode = val
			case float64: idcode = int64(val)
			}
		}
		if v := rec.ValueByKey("timeout_sec"); v != nil {
			switch val := v.(type) {
			case int64: timeout = val
			case float64: timeout = int64(val)
			}
		}
		if v := rec.ValueByKey("reconnect_sec"); v != nil {
			switch val := v.(type) {
			case int64: reconnect = val
			case float64: reconnect = int64(val)
			}
		}
		if v := rec.ValueByKey("lat"); v != nil {
			switch val := v.(type) {
			case float64: lat = val
			case int64: lat = float64(val)
			}
		}
		if v := rec.ValueByKey("lon"); v != nil {
			switch val := v.(type) {
			case float64: lon = val
			case int64: lon = float64(val)
			}
		}

		pmus = append(pmus, config.PMUConfig{
			Name:         name,
			IP:           ip,
			Port:         int(port),
			TCPPort:      int(tcpPort),
			IDCode:       uint16(idcode),
			Protocol:     protocol,
			TimeoutSec:   int(timeout),
			ReconnectSec: int(reconnect),
			Region:       region,
			Lat:          lat,
			Lon:          lon,
		})
	}
	if result.Err() != nil {
		return nil, result.Err()
	}
	return pmus, nil
}

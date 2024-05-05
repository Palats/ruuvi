package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/goccy/go-yaml"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	port           = flag.Int("port", 7361, "Port to serve on")
	debug          = flag.Bool("debug", false, "If true, export info about what was submitted")
	configFilename = flag.String("config", "", "YAML configuration file to use, optional")
	decodeData     = flag.String("decode_data", "", "Decode the provide bluetooth advertised data encoded in hex and exit. For debugging.")
)

var (
	tagMetricsNames = []string{
		"temperature", "pressure", "humidity",
		"accelx", "accely", "accelz",
		"voltage", "txpower", "rssi",
		"dataformat",
		"movementcounter",
		"measurementsequencenumber",
	}
	tagMetrics  map[string]*prometheus.GaugeVec
	tagUpdateAt = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ruuvi_updateat",
	}, []string{"name", "id"})
	tagStationBatteryLevel = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ruuvi_station_batterylevel",
	}, []string{"name", "id"})
	tagStationLocationAccuracy = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ruuvi_station_location_accuracy",
	}, []string{"name", "id"})
	tagStationLocationLatitude = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ruuvi_station_location_latitude",
	}, []string{"name", "id"})
	tagStationLocationLongitude = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ruuvi_station_location_longitude",
	}, []string{"name", "id"})
)

func init() {
	tagMetrics = map[string]*prometheus.GaugeVec{}
	for _, name := range tagMetricsNames {
		tagMetrics[name] = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ruuvi_" + name,
		}, []string{"name", "id"})
		prometheus.MustRegister(tagMetrics[name])
	}
	prometheus.MustRegister(tagUpdateAt)
	prometheus.MustRegister(tagStationBatteryLevel)
	prometheus.MustRegister(tagStationLocationAccuracy)
	prometheus.MustRegister(tagStationLocationLatitude)
	prometheus.MustRegister(tagStationLocationLongitude)
}

// Doc: https://github.com/ruuvi/com.ruuvi.station/wiki
// Example data in exampledata.json.

// StationInfo describes the format of update from RuuviStation.
// Doc at https://docs.ruuvi.com/ruuvi-station-app/gateway
// Example data in example-station.json.
type StationInfo struct {
	DeviceID     string
	EventID      string
	BatteryLevel int64
	Time         string // "2020-04-06T22:15:14+0200"
	Location     StationLocation
	Tags         []StationTag
}

// StationLocation .
type StationLocation struct {
	Accuracy  float64
	Latitude  float64
	Longitude float64
}

// StationTag is the info about the specific tag.
type StationTag struct {
	ID          string
	Name        string
	Pressure    float64
	Humidity    float64
	Temperature float64

	AccelX float64
	AccelY float64
	AccelZ float64

	UpdateAt                  string // "2020-04-09T15:01:59+0200
	DataFormat                int64
	DefaultBackground         int64
	Favorite                  bool
	MeasurementSequenceNumber int64
	MovementCounter           int64

	RSSI    int64
	TxPower float64
	Voltage float64

	RawDataBlob StationBlob
}

// StationBlob is the raw data of the sensors.
type StationBlob struct {
	Blob []int8
}

// BluetoothAdvertisement is the data contains in a given Ruuvi sensor
// bluetooth message.
// https://docs.ruuvi.com/communication/bluetooth-advertisements
type BluetoothAdvertisement struct {
	// Mandatory flags == 0x02 0x01 0x04 or 0x06 ?
	Flags [3]byte
	// Content length, == 0x1B == 27.
	// That's the content after this field - incl. type & manufacturer.
	Length byte
	// Type == 0xff
	Type byte
	// Manufacturer ID, least significant byte first: 0x0499 = Ruuvi Innovations Ltd
	Manufacturer uint16
	// Raw payload
	Payload []byte

	Data5 DataFormat5
}

// DataFormat5 represents the decoded values of a format 5 message.
// https://docs.ruuvi.com/communication/bluetooth-advertisements/data-format-5-rawv2
type DataFormat5 struct {
	// 0x5
	FormatVersion byte
	// Temperature in 0.005 degrees
	Temperature int16
	// Humidity in 0.0025% (0-163.83% range, though realistically 0-100%)
	Humidity uint16
	// Pressure (16bit unsigned) in 1 Pa units, with offset of -50000 Pa
	// i.e., actual pressure is this field + 50k Pa
	Pressure uint16

	// Acceleration, in milli-G
	AccelX int16
	AccelY int16
	AccelZ int16

	// Power info (11+5bit unsigned)
	// first 11 bits is the battery voltage above 1.6V, in millivolts (1.6V to 3.646V range).
	// Last 5 bits unsigned are the TX power above -40dBm, in 2dBm steps. (-40dBm to +20dBm range)
	// Probably invalid currently.
	CodedPower uint16
	Voltage    uint16
	RSSI       uint16

	// Incremented by motion detection interrupts from accelerometer
	MovementCounter byte
	// Each time a measurement is taken, this is incremented by one, used for measurement
	// de-duplication. Depending on the transmit interval, multiple packets with the same
	// measurements can be sent, and there may be measurements that never were sent.
	MeasureSequence uint16
	// 48bit MAC address
	MacAddress [6]byte
}

func decodeBluetoothData(raw string) (*BluetoothAdvertisement, error) {
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		return nil, err
	}

	lastIdx := -1
	consumeByte := func() (byte, error) {
		lastIdx++
		if len(decoded) <= lastIdx {
			return 0, fmt.Errorf("not enough data for index %d", lastIdx)
		}
		return decoded[lastIdx], nil
	}
	// Little endian
	consumeLEuint16 := func() (uint16, error) {
		b1, err := consumeByte()
		if err != nil {
			return 0, err
		}
		b2, err := consumeByte()
		if err != nil {
			return 0, err
		}
		return uint16(b1) + 256*uint16(b2), nil
	}
	// Big endian
	consumeBEuint16 := func() (uint16, error) {
		b1, err := consumeByte()
		if err != nil {
			return 0, err
		}
		b2, err := consumeByte()
		if err != nil {
			return 0, err
		}
		return uint16(b2) + 256*uint16(b1), nil
	}
	// Big endian
	consumeBEint16 := func() (int16, error) {
		b1, err := consumeByte()
		if err != nil {
			return 0, err
		}
		b2, err := consumeByte()
		if err != nil {
			return 0, err
		}
		return int16(b2) + 256*int16(b1), nil
	}

	var adv BluetoothAdvertisement

	// Parse flags
	if adv.Flags[0], err = consumeByte(); err != nil {
		return nil, err
	}
	if adv.Flags[1], err = consumeByte(); err != nil {
		return nil, err
	}
	if adv.Flags[2], err = consumeByte(); err != nil {
		return nil, err
	}

	// Parse length
	if adv.Length, err = consumeByte(); err != nil {
		return nil, err
	}
	if got, want := adv.Length, byte(27); got != want {
		return nil, fmt.Errorf("got 0x%x at index %d, wanted 0x%x", got, lastIdx, want)
	}

	// Parse type
	if adv.Type, err = consumeByte(); err != nil {
		return nil, err
	}
	if got, want := adv.Type, byte(0xff); got != want {
		return nil, fmt.Errorf("got 0x%x at index %d, wanted 0x%x", got, lastIdx, want)
	}

	// Parse manufacturer
	if adv.Manufacturer, err = consumeLEuint16(); err != nil {
		return nil, err
	}
	if got, want := adv.Manufacturer, uint16(0x0499); got != want {
		return nil, fmt.Errorf("got manufacturer ID 0x%x, wanted 0x%x", got, want)
	}

	// Get the rest of the payload
	// That does not advance lastIdx - we're just doing a copy here.
	adv.Payload = decoded[lastIdx+1:]
	if got, want := len(adv.Payload)+3, int(adv.Length); got != want {
		return nil, fmt.Errorf("got %d bytes for payload, while length indicates %d", got, want)
	}

	// Decode format v5
	if adv.Data5.FormatVersion, err = consumeByte(); err != nil {
		return nil, err
	}
	if got, want := adv.Data5.FormatVersion, byte(5); got != want {
		return nil, fmt.Errorf("got format version %d, wanted %d", got, want)
	}

	if adv.Data5.Temperature, err = consumeBEint16(); err != nil {
		return nil, err
	}
	if adv.Data5.Humidity, err = consumeBEuint16(); err != nil {
		return nil, err
	}
	if adv.Data5.Pressure, err = consumeBEuint16(); err != nil {
		return nil, err
	}
	if adv.Data5.AccelX, err = consumeBEint16(); err != nil {
		return nil, err
	}
	if adv.Data5.AccelY, err = consumeBEint16(); err != nil {
		return nil, err
	}
	if adv.Data5.AccelZ, err = consumeBEint16(); err != nil {
		return nil, err
	}

	// Power decoding seem off
	if adv.Data5.CodedPower, err = consumeLEuint16(); err != nil {
		return nil, err
	}
	adv.Data5.Voltage = adv.Data5.CodedPower & (1<<11 - 1)
	adv.Data5.RSSI = (adv.Data5.CodedPower >> 11) & (1<<5 - 1)
	if adv.Data5.MovementCounter, err = consumeByte(); err != nil {
		return nil, err
	}

	if adv.Data5.MeasureSequence, err = consumeBEuint16(); err != nil {
		return nil, err
	}
	for i := 0; i < 6; i++ {
		if adv.Data5.MacAddress[i], err = consumeByte(); err != nil {
			return nil, err
		}
	}

	return &adv, nil
}

// Config describes the accepted format for the config file.
type Config struct {
	// Gives override per tag. Keyed by the ID of the tag.
	Tags []*ConfigTagInfo `yaml:"tags"`
}

// ConfigTagInfo contains configuration per tag.
type ConfigTagInfo struct {
	// ID of the tag; serves as key here.
	ID string `yaml:"id"`

	// If not empty, use this name instead of the one provided by Ruuvi Station.
	Name string `yaml:"name"`
}

// Server takes care of receiving measures and export them back.
type Server struct {
	cfgPerTag map[string]*ConfigTagInfo

	m          sync.Mutex
	lastRaw    []byte
	lastParsed *StationInfo
}

// New creates a new server.
func New(cfg *Config) *Server {
	s := &Server{
		cfgPerTag: make(map[string]*ConfigTagInfo),
	}
	for _, tagCfg := range cfg.Tags {
		s.cfgPerTag[tagCfg.ID] = tagCfg
		fmt.Printf("Mapping %q to %q\n", tagCfg.ID, tagCfg.Name)
	}
	return s
}

// receive implements the endpoint receiving requests from the Ruuvi
// Station app.
func (s *Server) receive(_ http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		fmt.Printf("Read body error: %v\n", err)
		return
	}
	s.m.Lock()
	s.lastRaw = raw
	s.m.Unlock()

	data := &StationInfo{}
	if err := json.Unmarshal(raw, data); err != nil {
		fmt.Printf("Umarshal error: %v\n", err)
		return
	}
	s.m.Lock()
	s.lastParsed = data
	s.m.Unlock()

	for _, tag := range data.Tags {
		tagName := tag.Name
		if s.cfgPerTag[tag.ID] != nil && s.cfgPerTag[tag.ID].Name != "" {
			tagName = s.cfgPerTag[tag.ID].Name
		}
		if *debug {
			fmt.Printf("Tag %s: id=%q name=%q temp=%f pressure=%f humidity=%f\n", tagName, tag.ID, tag.Name, tag.Temperature, tag.Pressure, tag.Humidity)
		}

		v := reflect.ValueOf(tag)
		for _, metricName := range tagMetricsNames {
			// Generic fields attached to the tag.
			fv := v.FieldByNameFunc(func(fname string) bool {
				return strings.ToLower(fname) == metricName
			})
			var f float64
			if fv.Kind() == reflect.Int64 {
				f = float64(fv.Int())
			} else {
				f = fv.Float()
			}
			tagMetrics[metricName].With(prometheus.Labels{"name": tagName, "id": tag.ID}).Set(f)

			// Export updated time.
			var err error
			var t time.Time
			for _, timeFormat := range []string{"2006-01-02T15:04:05-0700", "2006-01-02T15:04:05-07:00"} {
				t, err = time.Parse(timeFormat, tag.UpdateAt)
				if err == nil {
					break
				}
			}
			if err != nil {
				fmt.Printf("Unable to parse %q: %v\n", tag.UpdateAt, err)
			} else {
				tagUpdateAt.With(prometheus.Labels{"name": tagName, "id": tag.ID}).Set(float64(t.Unix()))
			}

			// Export station info for each tag.
			tagStationBatteryLevel.With(prometheus.Labels{"name": tagName, "id": tag.ID}).Set(float64(data.BatteryLevel))
			tagStationLocationAccuracy.With(prometheus.Labels{"name": tagName, "id": tag.ID}).Set(data.Location.Accuracy)
			tagStationLocationLatitude.With(prometheus.Labels{"name": tagName, "id": tag.ID}).Set(data.Location.Latitude)
			tagStationLocationLongitude.With(prometheus.Labels{"name": tagName, "id": tag.ID}).Set(data.Location.Longitude)
		}
	}
}

// Serve .
func (s *Server) Serve(w http.ResponseWriter, r *http.Request) {
	if *debug {
		fmt.Printf("# Request method=%s, url=%s\n", r.Method, r.URL.String())
	}
	if r.Method == "POST" {
		s.receive(w, r)
	}

	s.m.Lock()
	data := map[string]interface{}{
		"LastRaw":        string(s.lastRaw),
		"LastParsed":     s.lastParsed,
		"LastParsedDump": spew.Sdump(s.lastParsed),
	}
	s.m.Unlock()

	var err error
	if *debug {
		err = indexDebugTpl.Execute(w, data)
	} else {
		err = indexTpl.Execute(w, data)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

var indexTpl = template.Must(template.New("index").Parse(`
<html><body>
Ruuvi Station proxy server.
</body></html>
`))

var indexDebugTpl = template.Must(template.New("index").Parse(`
<html><body>
<h1>Last parsed update</h1>
<pre>{{.LastParsedDump}}</pre>
<h1>Last raw</h1>
<pre>{{.LastRaw}}</pre>
</body></html>
`))

func main() {
	flag.Parse()

	if *decodeData != "" {
		adv, err := decodeBluetoothData(*decodeData)
		if err != nil {
			fmt.Fprintf(os.Stderr, "decoding failure: %v\n", err)
		}
		spew.Dump(adv)
		return
	}

	fmt.Println("Ruuvi gateway server")
	http.Handle("/metrics", promhttp.Handler())

	cfg := &Config{}
	if *configFilename != "" {
		raw, err := os.ReadFile(*configFilename)
		if err != nil {
			log.Fatalf("Unable to read %q: %v", *configFilename, err)
		}
		if err := yaml.Unmarshal(raw, cfg); err != nil {
			log.Fatalf("Unable to read %q: %v", *configFilename, err)
		}
	}

	s := New(cfg)
	http.HandleFunc("/", s.Serve)

	addr := fmt.Sprintf(":%d", *port)
	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}
	fmt.Printf("Listening on http://%s%s\n", hostname, addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

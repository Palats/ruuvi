package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	port  = flag.Int("port", 7361, "Port to serve on")
	debug = flag.Bool("debug", false, "If true, export info about what was submitted")
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
// Example: https://pastebin.com/ZpK0Nk2v

const timeFormat = "2006-01-02T15:04:05-0700"

// Info describes the format of update from RuuviStation.
type Info struct {
	DeviceID     string
	EventID      string
	BatteryLevel int64
	Time         string // "2020-04-06T22:15:14+0200"
	Location     InfoLocation
	Tags         []InfoTag
}

// InfoLocation .
type InfoLocation struct {
	Accuracy  float64
	Latitude  float64
	Longitude float64
}

// InfoTag is the info about the specific tag.
type InfoTag struct {
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

	RawDataBlob InfoBlob
}

// InfoBlob is the raw data of the sensors.
type InfoBlob struct {
	Blob []int8
}

// Server takes care of receiving measures and export them back.
type Server struct {
	m          sync.Mutex
	lastRaw    []byte
	lastParsed *Info
}

// New creates a new server.
func New() *Server {
	return &Server{}
}

// receive implements the endpoint receiving requests from the Ruuvi
// Station app.
func (s *Server) receive(w http.ResponseWriter, r *http.Request) {
	raw, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Printf("Read body error: %v\n", err)
		return
	}
	s.m.Lock()
	s.lastRaw = raw
	s.m.Unlock()

	data := &Info{}
	if err := json.Unmarshal(raw, data); err != nil {
		fmt.Printf("Umarshal error: %v\n", err)
		return
	}
	s.m.Lock()
	s.lastParsed = data
	s.m.Unlock()

	for _, tag := range data.Tags {
		fmt.Printf("Tag %s: temp=%f pressure=%f humidity=%f\n", tag.Name, tag.Temperature, tag.Pressure, tag.Humidity)
		v := reflect.ValueOf(tag)
		for _, name := range tagMetricsNames {
			// Generic fields attached to the tag.
			fv := v.FieldByNameFunc(func(fname string) bool {
				return strings.ToLower(fname) == name
			})
			var f float64
			if fv.Kind() == reflect.Int64 {
				f = float64(fv.Int())
			} else {
				f = fv.Float()
			}
			tagMetrics[name].With(prometheus.Labels{"name": tag.Name, "id": tag.ID}).Set(f)

			// Export updated time.
			t, err := time.Parse(timeFormat, tag.UpdateAt)
			if err != nil {
				fmt.Printf("Unable to parse %q: %v\n", tag.UpdateAt, err)
			} else {
				tagUpdateAt.With(prometheus.Labels{"name": tag.Name, "id": tag.ID}).Set(float64(t.Unix()))
			}

			// Export station info for each tag.
			tagStationBatteryLevel.With(prometheus.Labels{"name": tag.Name, "id": tag.ID}).Set(float64(data.BatteryLevel))
			tagStationLocationAccuracy.With(prometheus.Labels{"name": tag.Name, "id": tag.ID}).Set(data.Location.Accuracy)
			tagStationLocationLatitude.With(prometheus.Labels{"name": tag.Name, "id": tag.ID}).Set(data.Location.Latitude)
			tagStationLocationLongitude.With(prometheus.Labels{"name": tag.Name, "id": tag.ID}).Set(data.Location.Longitude)
		}
	}
}

// Serve .
func (s *Server) Serve(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("# Request method=%s, url=%s\n", r.Method, r.URL.String())
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

	fmt.Println("Ruuvi gateway server")
	http.Handle("/metrics", promhttp.Handler())
	s := New()
	http.HandleFunc("/", s.Serve)

	addr := fmt.Sprintf(":%d", *port)
	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}
	fmt.Printf("Listening on http://%s%s\n", hostname, addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

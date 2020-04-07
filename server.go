package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"sync"

	"github.com/davecgh/go-spew/spew"
)

// Example: https://pastebin.com/ZpK0Nk2v

// Info describes the format of update from RuuviStation.
type Info struct {
	DeviceID     string       `json:"deviceId"`
	EventID      string       `json:"eventId"`
	BatteryLevel int64        `json:"batteryLevel"`
	Time         string       `json:"time"` // "2020-04-06T22:15:14+0200"
	Location     InfoLocation `json:"location"`
	Tags         []InfoTag    `json:"tags"`
}

// InfoLocation .
type InfoLocation struct {
	Accuracy  float64 `json:"accuracy"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// InfoTag is the info about the specific tag.
type InfoTag struct {
	ID          string
	Name        string `json:"name"`
	Pressure    float64
	Humidity    float64 `json:"humidity"`
	Temperature float64

	AccelX float64
	AccelY float64
	AccelZ float64

	UpdateAt                  string
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

// InfoBlob is the raw data of the sensos.
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
	// fmt.Printf("Data:\n%s\n", string(raw))

	data := &Info{}
	if err := json.Unmarshal(raw, data); err != nil {
		fmt.Printf("Umarshal error: %v\n", err)
		return
	}
	s.m.Lock()
	s.lastParsed = data
	s.m.Unlock()
	// spew.Dump(data)
	for _, tag := range data.Tags {
		fmt.Printf("Tag %s: temp=%f pressure=%f humidity=%f\n", tag.Name, tag.Temperature, tag.Pressure, tag.Humidity)
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
	err := indexTpl.Execute(w, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

var indexTpl = template.Must(template.New("index").Parse(`
<html><body>
<h1>Last parsed update</h1>
<pre>{{.LastParsedDump}}</pre>
<h1>Last raw</h1>
<pre>{{.LastRaw}}</pre>
</body></html>
`))

func main() {
	fmt.Println("Ruuvi gateway server")
	s := New()
	http.HandleFunc("/", s.Serve)
	log.Fatal(http.ListenAndServe(":7361", nil))
}

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
)

// Example: https://pastebin.com/ZpK0Nk2v

type Info struct {
	DeviceID     string       `json:"deviceId"`
	EventID      string       `json:"eventId"`
	BatteryLevel int64        `json:"batteryLevel"`
	Time         string       `json:"time"` // "2020-04-06T22:15:14+0200"
	Location     InfoLocation `json:"location"`
	Tags         []InfoTag    `json:"tags"`
}

type InfoLocation struct {
	Accuracy  float64 `json:"accuracy"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

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

type InfoBlob struct {
	Blob []int8
}

func main() {
	fmt.Println("Ruuvi gateway server")
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("# Request...")
		raw, err := ioutil.ReadAll(r.Body)
		if err != nil {
			fmt.Printf("Read body error: %v\n", err)
			return
		}
		// fmt.Printf("Data:\n%s\n", string(raw))

		data := Info{}
		if err := json.Unmarshal(raw, &data); err != nil {
			fmt.Printf("Umarshal error: %v\n", err)
			return
		}
		// spew.Dump(data)
		for _, tag := range data.Tags {
			fmt.Printf("Tag %s: temp=%f pressure=%f humidity=%f\n", tag.Name, tag.Temperature, tag.Pressure, tag.Humidity)
		}
	})
	log.Fatal(http.ListenAndServe(":8080", nil))
}

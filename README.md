# Ruuvi proxy server

This server accepts data from Ruuvi sensors and exports the data to a `/metrics` endpoint suitable for [Prometheus](https://prometheus.io/). It supports:
 - The [Ruuvi Station Android app](https://play.google.com/store/apps/details?id=com.ruuvi.station).
 - The Ruuvi Gateway HTTP export.

## Usage

 * Build the binary. There are a few ways to do that:
   * Using `go build server.go` which will create a `server` binary in the same directory.
   * Using the provided docker file: `docker build -t ruuvi .`
 * Then you need to run server at an address accessible from your Ruuvi Station app:
   * Running the binary directly: `./server`
   * Running through docker: `docker run -it --rm ruuvi`
   * In both cases, the default port is `7361`, which can be changed with the `--port xxxx` flag.
 * In the Ruuvi Station app, in `App Settings` / `Gateway Settings`, set `Gateway URL` to the address of the server you have configured.
 * You probably want to activate `Background Scanning` also - otherwise it won't send data regularly. You might want to activate `Keep the device awake` also.
 * You can check that the data is there by looking at the `/metrics` endpoint.
 * Config Prometheus to scrape your server.


## Notes

 * `example-station.json`: Example of data sent by Ruuvi Station App gateway mode; https://docs.ruuvi.com/ruuvi-station-app/gateway
 * `example-gateway-http.json`: Example of data sent by Ruuvi Gateway; https://docs.ruuvi.com/gw-data-formats/http-time-stamped-data-from-bluetooth-sensors
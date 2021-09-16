package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/go-ping/ping"
	"github.com/gorilla/mux"
	"github.com/influxdata/influxdb-client-go/v2"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/tcnksm/go-httpstat"
	"golang.org/x/tools/container/intsets"
)

type Configuration struct {
	Debug      bool                `json:"debug"`
	Database   InfluxConfiguration `json:"database"`
	Probes     []Probe             `json:"probes"`
	Privileged bool                `json:"privileged"`
	Targets    map[string]Target   `json:"targets"`
}

type InfluxConfiguration struct {
	Host   string `json:"host"`
	Token  string `json:"token"`
	Org    string `json:"org"`
	Bucket string `json:"bucket"`
}

type ErrorResponse struct {
	Errors []*ErrorPacket `json:"errors"`
}

type ErrorPacket struct {
	Status string `json:"status"`
	Source string `json:"source"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type Probe struct {
	Name    string   `json:"name"`
	Secret  string   `json:"secret"`
	Targets []string `json:"targets"`
}

type ResponsePacket struct {
	ProbeName  string    `json:"probe_name"`
	TargetName string    `json:"target_name"`
	ProbeType  string    `json:"probe_type"`
	MinRTT     int64     `json:"min_rtt"`
	MaxRTT     int64     `json:"max_rtt"`
	Median     int64     `json:"median"`
	NumProbes  int       `json:"num_probes"`
	Timestamp  time.Time `json:"timestamp"`
}

type Target struct {
	Name      string `json:"name"`
	Host      string `json:"host"`
	ProbeType string `json:"probe_type"`
	Probes    int    `json:"probes"`
	Intervall int    `json:"intervall"`
}

var Config Configuration
var Client influxdb2.Client

const version = "0.0.1"
const apiVersion = "0.0.1"

func main() {

	log.SetLevel(log.InfoLevel)

	log.SetFormatter(&log.TextFormatter{
		DisableColors: true,
		FullTimestamp: true,
	})

	log.Printf("Running version: %s", version)

	configFile := flag.String("config", "config/config.json", "config file")
	debug := flag.Bool("debug", false, "enable debug mode")
	mode := flag.String("mode", "head", "head / probe")
	headNode := flag.String("head", "", "fqdn / ip of head node")
	privileged := flag.Bool("privileged", false, "enable privileged mode")
	probeName := flag.String("name", "", "name of probe")

	flag.Parse()

	if *debug {
		log.SetLevel(log.DebugLevel)
		Config.Debug = true
	}

	if *privileged {
		Config.Privileged = true
	}

	log.Debugf("config:", *configFile)
	log.Debugf("mode:", *mode)

	if *mode == "head" {

		parseConfig(configFile)

		Client = influxdb2.NewClient(Config.Database.Host, Config.Database.Token)
		defer Client.Close()

		router := mux.NewRouter()

		// make use of our middleware to set content type and such
		router.Use(commonMiddleware)

		router.HandleFunc("/version", VersionRequest).Methods("GET")
		router.HandleFunc("/probes/{name}", GetProbe).Methods("GET")
		router.HandleFunc("/targets/{name}", SubmitTarget).Methods("POST")
		log.Fatal(http.ListenAndServe(":8000", router))
	} else {
		request, _ := http.NewRequest("GET", *headNode+"probes/"+*probeName, nil)
		request.Header.Set("X-Authorization", os.Getenv("NPROBE_SECRET"))
		client := &http.Client{}
		response, err := client.Do(request)
		if err != nil {
			log.Fatalf("Error retrieving configuration from head: %s\n", err)
		} else {
			data, _ := ioutil.ReadAll(response.Body)
			log.Debugf("Config received:\n%s", data)
			var targets []Target
			err := json.Unmarshal(data, &targets)

			log.Printf("Received targets: %+v", targets)

			if err != nil {
				log.Fatalf("Error while processing configuration: %s", err)
			}

			var wg sync.WaitGroup

			for _, k := range targets {
				wg.Add(1)
				go HandleProbe(k, *headNode, *probeName, &wg)
			}
			wg.Wait()
		}
	}

}

func HandleProbe(k Target, headnode string, probeName string, wg *sync.WaitGroup) {
	for {

		var r = ResponsePacket{}

		if k.ProbeType != "icmp" && k.ProbeType != "http" {
			r = runExternalProbe(k.Host, k.Probes, k.ProbeType)
		}
		if k.ProbeType == "icmp" {
			r = probeIcmp(k.Host, k.Probes)
		}
		if k.ProbeType == "http" {
			r = probeHttp(k.Host, k.Probes)
		}
		r.TargetName = k.Name
		r.ProbeType = k.ProbeType
		r.ProbeName = probeName

		url := headnode + "targets/" + k.Name

		jsonValue, _ := json.Marshal(r)
		request2, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonValue))
		request2.Header.Set("X-Authorization", os.Getenv("NPROBE_SECRET"))
		client2 := &http.Client{}
		body, err := client2.Do(request2)
		if err != nil {
			fmt.Printf("The HTTP request failed with error %s\n", err)
		}

		log.Printf("%+v", body)

		time.Sleep(time.Duration(k.Intervall) * time.Second)
	}
	defer wg.Done()
}

func GetProbe(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	for _, probe := range Config.Probes {
		if probe.Name == params["name"] {
			if r.Header.Get("X-Authorization") == probe.Secret {

				var targets []Target = make([]Target, len(probe.Targets))

				var i = 0

				for _, k := range probe.Targets {
					targets[i] = Config.Targets[k]
					i++
				}

				json.NewEncoder(w).Encode(targets)
				return
			} else {
				handleError(w, http.StatusForbidden, r.RequestURI, "You're not allowed here", nil)
				return
			}
		}
	}
	handleError(w, http.StatusBadRequest, r.RequestURI, "Misformed payload", nil)
}

func SubmitTarget(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)

	log.Debugf("%+v", params)

	var responsePacket ResponsePacket
	_ = json.NewDecoder(r.Body).Decode(&responsePacket)

	log.Debugf("%+v", responsePacket)

	// user blocking write client for writes to desired bucket
	writeAPI := Client.WriteAPI(Config.Database.Org, Config.Database.Bucket)
	// create point using fluent style
	p := influxdb2.NewPointWithMeasurement("stat").
		AddTag("unit", "milliseconds").
		AddTag("target", responsePacket.TargetName).
		AddTag("probe", responsePacket.ProbeName).
		AddField("avg", responsePacket.Median).
		AddField("max", responsePacket.MaxRTT).
		AddField("min", responsePacket.MinRTT).
		SetTime(responsePacket.Timestamp)
	writeAPI.WritePoint(p)
}

func probeIcmp(hostname string, probes int) ResponsePacket {
	pinger, err := ping.NewPinger(hostname)
	if err != nil {
		log.Errorf("Pinger error: %s\n", err)
	}
	pinger.Count = probes

	pinger.SetPrivileged(Config.Privileged)

	if Config.Debug {
		pinger.Debug = true
	}

	err = pinger.Run() // blocks until finished
	if err != nil {
		log.Errorf("Pinger error: %s\n", err)
	}

	stats := pinger.Statistics() // get send/receive/rtt stats

	r := ResponsePacket{MinRTT: stats.MinRtt.Nanoseconds() / 1000000,
		MaxRTT:    stats.MaxRtt.Nanoseconds() / 1000000,
		Median:    stats.AvgRtt.Nanoseconds() / 1000000,
		NumProbes: probes,
		Timestamp: time.Now()}

	return r
}

func probeHttp(url string, probes int) ResponsePacket {
	i := 0

	min := intsets.MaxInt
	max := 0
	avg := 0

	for {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			log.Fatal(err)
		}
		// Create a httpstat powered context
		var result httpstat.Result
		ctx := httpstat.WithHTTPStat(req.Context(), &result)
		req = req.WithContext(ctx)
		// Send request by default HTTP client
		client := http.DefaultClient
		res, err := client.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := io.Copy(ioutil.Discard, res.Body); err != nil {
			log.Fatal(err)
		}
		res.Body.Close()

		con := int(result.TCPConnection / time.Millisecond)

		if con < min {
			min = con
		}
		if con > max {
			max = con
		}
		avg += con

		i++
		if i == probes {
			avg = avg / probes
			break
		}
	}

	r := ResponsePacket{MinRTT: int64(min),
		MaxRTT:    int64(max),
		Median:    int64(avg),
		NumProbes: probes,
		Timestamp: time.Now()}

	return r
}

func runExternalProbe(host string, probes int, probe string) ResponsePacket {

	r := ResponsePacket{MinRTT: int64(0),
		MaxRTT:    int64(0),
		Median:    int64(0),
		NumProbes: probes}

	return r
}

func handleError(w http.ResponseWriter, status int, source string, title string, err error) {
	errorResponse := ErrorResponse{Errors: []*ErrorPacket{
		&ErrorPacket{
			Status: strconv.Itoa(status),
			Source: source,
			Title:  title,
			Detail: fmt.Sprintf("%v", err)}}}

	e, _ := json.Marshal(errorResponse)

	http.Error(w, string(e[:]), status)
}

func commonMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dumpRequest(r)
		w.Header().Add("Content-Type", "application/vnd.api+json")
		w.Header().Add("X-Api-Version", apiVersion)
		w.Header().Add("X-Powered-By", "nprobe")
		next.ServeHTTP(w, r)
	})
}

func VersionRequest(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(fmt.Sprintf("{ \"Version:\" \"%s\" }", version)))
}

func dumpRequest(r *http.Request) {
	if log.GetLevel() == log.DebugLevel {
		requestDump, err := httputil.DumpRequest(r, true)
		if err != nil {
			log.Errorf("Failed to dump http request '%s", err)
		} else {
			log.Debugf("%s", string(requestDump))
		}
	}
}

func parseConfig(configPtr *string) {
	viper.Set("Verbose", true)
	viper.SetConfigFile(*configPtr) // name of config file (without extension)
	viper.SetConfigType("json")
	err := viper.ReadInConfig() // Find and read the config file

	if err != nil { // Handle errors reading the config file
		log.Fatalf("Fatal error config file: %s \n", err)
	}

	log.Infof("Using config: %s\n", viper.ConfigFileUsed())

	err = viper.Unmarshal(&Config)
	if err != nil {
		log.Fatalf("unable to decode into struct, %v", err)
	}

	log.Debugf("%+v", Config)
}

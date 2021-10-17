package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/influxdata/influxdb-client-go/v2"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type Configuration struct {
	Authorization string               `mapstructure:"authorization"`
	Debug         bool                 `mapstructure:"debug"`
	Database      InfluxConfiguration  `mapstructure:"database"`
	Satellites    map[string]Satellite `mapstructure:"satellites"`
	Privileged    bool                 `mapstructure:"privileged"`
	Targets       map[string]Target    `mapstructure:"targets"`
}

type InfluxConfiguration struct {
	Host   string `mapstructure:"host"`
	Token  string `mapstructure:"token"`
	Org    string `mapstructure:"org"`
	Bucket string `mapstructure:"bucket"`
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

type Satellite struct {
	Name        string   `mapstructure:"name"`
	Secret      string   `mapstructure:"secret"`
	Targets     []string `mapstructure:"targets"`
	LastData	time.Time `mapstructure:"last_data"`
	Health	    bool	`mapstructure:"health"`
}

type ResponsePacket struct {
	SatelliteName string  `mapstructure:"satellite_name"`
	TargetName    string  `mapstructure:"target_name"`
	ProbeType     string  `mapstructure:"probe_type"`
	Probes        []Probe `mapstructure:"probes"`
}

type Probe struct {
	MinRTT    int64     `mapstructure:"min_rtt"`
	MaxRTT    int64     `mapstructure:"max_rtt"`
	Median    int64     `mapstructure:"median"`
	NumProbes int       `mapstructure:"num_probes"`
	Timestamp time.Time `mapstructure:"timestamp"`
}

type Target struct {
	Name      string `mapstructure:"name"`
	Host      string `mapstructure:"host"`
	ProbeType string `mapstructure:"probe_type"`
	Probes    int    `mapstructure:"probes"`
	Interval  int    `mapstructure:"interval"`
	BatchSize int    `mapstructure:"batch_size"`
}

var Config Configuration
var ConfigFile string
var Client influxdb2.Client

const version = "0.0.1"
const apiVersion = "0.0.1"

func main() {

	log.SetLevel(log.InfoLevel)

	log.SetFormatter(&log.TextFormatter{
		DisableColors: true,
		FullTimestamp: true,
	})

	hostname, err := os.Hostname()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	configFile := flag.String("config", "config/config.json", "config file")
	debug := flag.Bool("debug", false, "enable debug mode")
	mode := flag.String("mode", "satellite", "head / satellite")
	headNode := flag.String("head", "", "fqdn / ip of head node")
	privileged := flag.Bool("privileged", false, "enable privileged mode")
	probeName := flag.String("name", hostname, "name of probe")
	notls := flag.Bool("notls", false, "disable use of tls")
	insecureTls := flag.Bool("insecure-tls", false, "disable use of tls cert checking")

	flag.Parse()

	if *debug {
		log.SetLevel(log.DebugLevel)
		Config.Debug = true
	}

	if *privileged {
		Config.Privileged = true
	}

	log.Printf("Host '%s' running version: %s", *probeName, version)
	log.Debugf("mode: %s", *mode)

	if *mode == "head" {

		parseConfig(configFile)
		ConfigFile = *configFile

		Client = influxdb2.NewClient(Config.Database.Host, Config.Database.Token)
		defer Client.Close()

		router := mux.NewRouter()

		// make use of our middleware to set content type and such
		router.Use(commonMiddleware)

		router.HandleFunc("/config", ConfigReload).Headers("X-Authorization", Config.Authorization).Methods("POST")
		router.HandleFunc("/healthz", HealthRequest).Methods("GET")
		router.HandleFunc("/satellites/{name}/targets", GetTargets).Methods("GET")
		router.HandleFunc("/targets/{name}", SubmitTarget).Methods("POST")
		router.HandleFunc("/version", VersionRequest).Methods("GET")
		log.Fatal(http.ListenAndServe(":8000", router))
	} else {

		headUrl := "https://"

		if *notls {
			headUrl = "http://"
		}

		headUrl = headUrl + *headNode + "/"

		request, _ := http.NewRequest("GET", headUrl +"satellites/"+*probeName+"/targets", nil)
		request.Header.Set("X-Authorization", os.Getenv("NPROBE_SECRET"))

		t := &http.Transport{}

		if !*insecureTls {
			t = &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			}
		}
		client := &http.Client{Transport: t, Timeout: 15 * time.Second}

		response, err := client.Do(request)
		if err != nil {
			log.Fatalf("Error retrieving configuration from head: %s\n", err)
		} else {

			if response.StatusCode != 200 {
				if response.StatusCode == 404 {
					log.Fatalf("Error talking to head - validate that your satellite name is correct: %s", response.Status)
				}
				if response.StatusCode == 403 {
					log.Fatalf("Error talking to head - validate that your authorization is correct: %s", response.Status)
				}
				log.Fatalf("Error talking to head: %s", response.Status)
			}

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
				go HandleProbe(k, headUrl, *probeName, &wg)
			}
			wg.Wait()
		}
	}

}

func ConfigReload(w http.ResponseWriter, r *http.Request) {

	log.Infof("Config Reload triggered")
	parseConfig(&ConfigFile)

}

func GetTargets(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)

	satellite, found := Config.Satellites[params["name"]]

	if ! found {
		handleError(w, http.StatusNotFound, r.RequestURI, "Requested item not found", nil)
		return
	}

	if r.Header.Get("X-Authorization") == satellite.Secret {

		  var targets = make([]Target, len(satellite.Targets))

		  var i = 0

		  for _, k := range satellite.Targets {
			  targets[i] = Config.Targets[k]
			  i++
		  }

		  log.Debugf("Satellite '%s' is receiving these targets: %+v", satellite.Name, targets)

		  err := json.NewEncoder(w).Encode(targets)

		  if err != nil {
			  log.Errorf("Error while encoding targets: %s", err)
		  }
		  return
	} else {
		  handleError(w, http.StatusForbidden, r.RequestURI, "You're not allowed here", nil)
		  return
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

	for _, probe := range responsePacket.Probes {
		p := influxdb2.NewPointWithMeasurement("stat").
			AddTag("unit", "milliseconds").
			AddTag("target", responsePacket.TargetName).
			AddTag("probe", responsePacket.SatelliteName).
			AddField("avg", probe.Median).
			AddField("max", probe.MaxRTT).
			AddField("min", probe.MinRTT).
			SetTime(probe.Timestamp)
		writeAPI.WritePoint(p)
	}

	s := Config.Satellites[responsePacket.SatelliteName]
	s.LastData = time.Now()

	Config.Satellites[responsePacket.SatelliteName] = s

	log.Debugf("Satellite data: %+v", s)
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

func HealthRequest(w http.ResponseWriter, r *http.Request) {

	log.Infof("Running Health-Check")
	msg := "Health Check not ok"

	authHeader := r.Header.Get("X-Authorization")
	authedRequest := false

	if authHeader == Config.Authorization {
		authedRequest = true
	}

	health, err := Client.Health(context.Background())

	if err != nil {

		log.Printf("Influx Health Check failed: %s", err)

		if authedRequest {
			 if health != nil {
				 msg = fmt.Sprintf("Influx Error: %s", health.Message)
			 }
		} else {
			 // for unauthed requests to /health we don't want to leak the actual error
			 err = nil
		}

		handleError(w, http.StatusServiceUnavailable, "/healthz", msg, err)
		return
	}

	log.Info("Health-Check completed OK")
}


func VersionRequest(w http.ResponseWriter, r *http.Request) {
	_, err := w.Write([]byte(fmt.Sprintf("{ \"Version:\" \"%s\" }", version)))

	if err != nil {
		log.Errorf("Error while writing to client: %s", err)
	}
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
	if Config.Debug {
		viper.Set("Verbose", true)
	}
	viper.SetConfigFile(*configPtr) // name of config file (without extension)
	viper.SetConfigType("json")
	err := viper.ReadInConfig() // Find and read the config file

	if err != nil { // Handle errors reading the config file
		log.Fatalf("Fatal error config file: %s \n", err)
	}

	log.Infof("Using config file: %s\n", viper.ConfigFileUsed())

	err = viper.Unmarshal(&Config)
	if err != nil {
		log.Fatalf("unable to decode into struct, %v", err)
	}

	// inject name from map names
	for name, k := range Config.Targets {
		k.Name = name
		Config.Targets[name] = k
	}

	log.Debugf("%+v", Config)
}

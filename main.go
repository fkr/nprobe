package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/influxdata/influxdb-client-go/v2"
	"github.com/sirupsen/logrus"
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
	Active   bool      `mapstructure:"active"`
	Name     string    `mapstructure:"name"`
	Secret   string    `mapstructure:"secret" json:"-"`
	Targets  []string  `mapstructure:"targets"`
	LastData time.Time `mapstructure:"last_data"`
	Health   bool      `mapstructure:"health"`
}

type ResponsePacket struct {
	SatelliteName string  `mapstructure:"satellite_name"`
	TargetName    string  `mapstructure:"target_name"`
	ProbeType     string  `mapstructure:"probe_type"`
	Probes        []Probe `mapstructure:"probes"`
}

type Probe struct {
	MinRTT    float64   `mapstructure:"min_rtt"`
	MaxRTT    float64   `mapstructure:"max_rtt"`
	Median    float64   `mapstructure:"median"`
	StdDev    float64   `mapstructure:"stddev"`
	Loss      float64   `mapstructure:"loss"`
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

type Worker struct {
	Target    Target
	HeadUrl   string
	ProbeName string
	Id        int
	Err       error
}

var Config Configuration
var ConfigFile string
var Client influxdb2.Client
var log *logrus.Logger

const version = "0.0.2"
const apiVersion = "0.0.1"

func main() {

	log = logrus.New()

	log.SetLevel(logrus.InfoLevel)

	log.SetFormatter(&logrus.TextFormatter{
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
	headNode := flag.String("head", "", "fqdn / ip of head node")
	insecureTls := flag.Bool("insecure-tls", false, "disable use of tls cert checking")
	mode := flag.String("mode", "satellite", "head / satellite")
	notls := flag.Bool("notls", false, "disable use of tls")
	privileged := flag.Bool("privileged", false, "enable privileged mode")
	probeName := flag.String("name", hostname, "name of probe")

	flag.Parse()

	if *debug {
		log.SetLevel(logrus.DebugLevel)
		Config.Debug = true
	}

	if *privileged {
		Config.Privileged = true
	}

	if *headNode == "" {
		*mode = "head"
	}

	log.WithFields(logrus.Fields{
		"host":    *probeName,
		"version": version,
		"mode":    *mode,
	}).Info("nprobe is starting")

	if *mode == "head" {

		parseConfig(configFile)
		ConfigFile = *configFile

		if Config.Database.Host != "" {
			Client = influxdb2.NewClient(Config.Database.Host, Config.Database.Token)
			defer Client.Close()
		} else {
			log.Warn("No Database configuration")
		}

		router := mux.NewRouter()

		// make use of our middleware to set content type and such
		router.Use(commonMiddleware)

		router.HandleFunc("/config", ConfigReload).Headers("X-Authorization", Config.Authorization).Methods("POST")
		router.HandleFunc("/healthz", HealthRequest).Methods("GET")
		router.HandleFunc("/satellites/{name}", GetSatellite).Methods("GET")
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

		request, _ := http.NewRequest("GET", headUrl+"satellites/"+*probeName+"/targets", nil)
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
			log.WithFields(logrus.Fields{"error": err}).Fatal("Error retrieving configuration from head")
		} else {
			if response.StatusCode != 200 {
				errorMsg, _ := ioutil.ReadAll(response.Body)
				switch response.StatusCode {
				case 403:
					log.WithFields(logrus.Fields{"Response Status": response.StatusCode}).
						Error("Error talking to head - validate that your authorization is correct")
				case 404:
					log.WithFields(logrus.Fields{"Response Status": response.StatusCode}).
						Error("Error talking to head - validate that your satellite name is correct")
				default:
					log.WithFields(logrus.Fields{"Response Status": response.StatusCode}).
						Error("Error talking to head")
				}
				log.WithFields(logrus.Fields{"Raw Error Message": errorMsg}).
					Debug("Error talking to head")
				log.Fatal("Abort - critical error")
			}

			data, _ := ioutil.ReadAll(response.Body)
			var targets []Target
			err := json.Unmarshal(data, &targets)

			if err != nil {
				log.WithFields(logrus.Fields{"error": err}).Fatal("Error while processing configuration")
			}

			log.WithFields(logrus.Fields{
				"targets": targets,
			}).Infof("Targets received")

			log.WithFields(logrus.Fields{"configuration": data}).Debug("Configuration received")

			workerChan := make(chan *Worker, len(targets))
			i := 0
			for _, k := range targets {
				wk := &Worker{
					Target:    k,
					HeadUrl:   headUrl,
					ProbeName: *probeName,
					Id:        i}

				log.WithFields(logrus.Fields{
					"worker id": i,
					"target":    wk.Target.Name,
					"type":      wk.Target.ProbeType,
				}).Info("Launching worker")
				go wk.HandleProbe(workerChan)
				i++
				// put a few seconds in between starting the worker
				time.Sleep(5 * time.Second)
			}

			// read the channel, it will block until something is written, then a new
			// goroutine will start
			for wk := range workerChan {
				// log the error
				log.WithFields(logrus.Fields{
					"worker id": wk.Id,
					"target":    wk.Target.Name,
					"error":     wk.Err,
				}).Error()
				// reset err
				wk.Err = nil
				// a goroutine has ended, restart it
				go wk.HandleProbe(workerChan)
			}
		}
	}
}

func ConfigReload(w http.ResponseWriter, r *http.Request) {
	log.Infof("Config Reload triggered")
	parseConfig(&ConfigFile)
}

func GetSatellite(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)

	satellite, found := Config.Satellites[params["name"]]

	if !found {
		handleError(w, http.StatusNotFound, r.RequestURI, "Requested item not found", nil)
		return
	}

	if r.Header.Get("X-Authorization") == satellite.Secret {
		err := json.NewEncoder(w).Encode(satellite)

		if err != nil {
			log.WithFields(logrus.Fields{"error": err}).Error()
		}
		return
	} else {
		handleError(w, http.StatusForbidden, r.RequestURI, "You're not allowed here", nil)
		return
	}
}

func GetTargets(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)

	satellite, found := Config.Satellites[params["name"]]

	if !found {
		handleError(w, http.StatusNotFound, r.RequestURI, "Requested item not found", nil)
		return
	}

	if r.Header.Get("X-Authorization") != satellite.Secret {
		handleError(w, http.StatusForbidden, r.RequestURI, "You're not allowed here", nil)
		return
	}

	if !satellite.Active {
		handleError(w, http.StatusForbidden, r.RequestURI, "You're not allowed here",
			errors.New("satellite marked inactive"))
		return
	}

	var targets = make([]Target, len(satellite.Targets))
	var i = 0

	for _, k := range satellite.Targets {
		targets[i] = Config.Targets[k]
		i++
	}

	log.WithFields(logrus.Fields{
		"satellite": satellite.Name,
		"targets":   targets,
	}).Debugf("Satellite is receiving targets")

	err := json.NewEncoder(w).Encode(targets)

	if err != nil {
		handleError(w, http.StatusServiceUnavailable, r.RequestURI, "Error while encoding targets", err)
	}
	return
}

func SubmitTarget(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)

	log.Debugf("%+v", params)

	var responsePacket ResponsePacket
	_ = json.NewDecoder(r.Body).Decode(&responsePacket)

	log.WithFields(logrus.Fields{"responsePacket": responsePacket}).Debug()

	satellite := Config.Satellites[responsePacket.SatelliteName]

	if r.Header.Get("X-Authorization") != satellite.Secret {
		handleError(w, http.StatusForbidden, r.RequestURI, "You're not allowed here", nil)
		return
	}

	if !satellite.Active {
		handleError(w, http.StatusForbidden, r.RequestURI, "You're not allowed here - satellite is marked inactive", nil)
		return
	}

	writeData(responsePacket)

	s := Config.Satellites[responsePacket.SatelliteName]
	s.LastData = time.Now()
	Config.Satellites[responsePacket.SatelliteName] = s

	log.WithFields(logrus.Fields{"data": s}).Debug()
}

func writeData(responsePacket ResponsePacket) {

	if Config.Database.Host != "" {
		// user blocking write client for writes to desired bucket
		writeAPI := Client.WriteAPI(Config.Database.Org, Config.Database.Bucket)
		// create point using fluent style

		for _, probe := range responsePacket.Probes {
			p := influxdb2.NewPointWithMeasurement("stat").
				AddTag("unit", "milliseconds").
				AddTag("target", responsePacket.TargetName+" ("+responsePacket.ProbeType+")").
				AddTag("probe", responsePacket.SatelliteName).
				AddField("stddev", probe.StdDev).
				AddField("median", probe.Median).
				AddField("max", probe.MaxRTT).
				AddField("min", probe.MinRTT).
				AddField("loss", probe.Loss).
				SetTime(probe.Timestamp)
			writeAPI.WritePoint(p)
		}
	}
}

func handleError(w http.ResponseWriter, status int, source string, title string, err error) {
	log.WithFields(logrus.Fields{
		"error": err,
	}).Error("Error while encoding targets")

	errorResponse := ErrorResponse{Errors: []*ErrorPacket{
		{
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
		w.Header().Add("X-Nprobe-Version", version)
		w.Header().Add("X-Powered-By", "nprobe")
		next.ServeHTTP(w, r)
	})
}

func HealthRequest(w http.ResponseWriter, r *http.Request) {
	log.Info("Running Health-Check")
	msg := "Health Check not ok"

	authHeader := r.Header.Get("X-Authorization")
	authedRequest := false

	if authHeader == Config.Authorization {
		authedRequest = true
	}

	health, err := Client.Health(context.Background())

	if err != nil {
		log.WithFields(logrus.Fields{"error": err}).Error()

		if authedRequest {
			if health != nil {
				msg = fmt.Sprintf("Influx Error: %s", *health.Message)
			}
		} else {
			// for unauthed requests to /health we don't want to leak the actual error
			err = nil
		}

		handleError(w, http.StatusServiceUnavailable, "/healthz", msg, err)
		return
	}

	// check each satellite
	for _, k := range Config.Satellites {
		if k.Active {
			last := k.LastData

			// find interval
			interval := math.MaxInt
			for _, t := range k.Targets {
				s := Config.Targets[t]
				if s.Interval < interval {
					interval = s.Interval
				}
			}

			timeout := time.Now().Add(-time.Minute * time.Duration(int64(interval)))
			if last.Before(timeout) {

				if authedRequest {
					msg = fmt.Sprintf("Probe '%s' has not come back in time. Last message from '%s'", k.Name, k.LastData)
				} else {
					msg = ""
				}

				handleError(w, http.StatusServiceUnavailable, "/healthz", msg, err)
				return
			}
		}
	}

	log.Info("Health-Check completed OK")
}

func VersionRequest(w http.ResponseWriter, r *http.Request) {
	_, err := w.Write([]byte(fmt.Sprintf("{ \"Version:\" \"%s\" }", version)))

	if err != nil {
		log.WithFields(logrus.Fields{"error": err}).Error()
	}
}

func dumpRequest(r *http.Request) {
	if log.GetLevel() == logrus.DebugLevel {
		requestDump, err := httputil.DumpRequest(r, true)
		if err != nil {
			log.WithFields(logrus.Fields{"error": err}).Error()
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
		log.WithFields(logrus.Fields{"error": err}).Fatal("Error while processing configuration")
	}

	log.Infof("Using config file: %s\n", viper.ConfigFileUsed())
	err = viper.Unmarshal(&Config)
	if err != nil {
		log.WithFields(logrus.Fields{"error": err}).Fatal("Error while unmarshalling")
	}

	// inject name from map names
	for name, s := range Config.Satellites {
		s.Name = name
		Config.Satellites[name] = s
	}
	for name, k := range Config.Targets {
		k.Name = name
		Config.Targets[name] = k
	}

	log.Debugf("%+v", Config)
}

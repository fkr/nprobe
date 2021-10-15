package main

import (
	"bytes"
	"crypto/tls"
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
	Authorization string              `mapstructure:"authorization"`
	Debug         bool                `mapstructure:"debug"`
	Database      InfluxConfiguration `mapstructure:"database"`
	Satellites    []Satellite         `mapstructure:"satellites"`
	Privileged    bool                `mapstructure:"privileged"`
	Targets       map[string]Target   `mapstructure:"targets"`
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
	Name    string   `mapstructure:"name"`
	Secret  string   `mapstructure:"secret"`
	Targets []string `mapstructure:"targets"`
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
		router.HandleFunc("/version", VersionRequest).Methods("GET")
		router.HandleFunc("/satellites/{name}", GetProbe).Methods("GET")
		router.HandleFunc("/targets/{name}", SubmitTarget).Methods("POST")
		log.Fatal(http.ListenAndServe(":8000", router))
	} else {

		headUrl := "https://"

		if !*notls {
			headUrl = "http://"
		}

		request, _ := http.NewRequest("GET", headUrl+*headNode+"/satellites/"+*probeName, nil)
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

func ConfigReload(w http.ResponseWriter, r *http.Request) {

	log.Infof("Config Reload triggered")
	parseConfig(&ConfigFile)

}

func HandleProbe(k Target, headnode string, probeName string, wg *sync.WaitGroup) {
	defer wg.Done()
	for {

		var r = ResponsePacket{}

		//if k.ProbeType != "icmp" && k.ProbeType != "http" {
		//	r = runExternalProbe(k.Host, k.Satellites, k.ProbeType)
		//}
		if k.ProbeType == "icmp" {
			r = k.ProbeIcmp(probeName)
		}
		if k.ProbeType == "http" {
			r = k.probeHttp(probeName)
		}

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

		time.Sleep(time.Duration(k.Interval) * time.Second)
	}
}

func GetProbe(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	for _, satellite := range Config.Satellites {
		if satellite.Name == params["name"] {
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
}

func (target *Target) ProbeIcmp(probeName string) ResponsePacket {

	probes := make([]Probe, target.BatchSize)
	pinger, err := ping.NewPinger(target.Host)
	if err != nil {
		log.Errorf("Pinger error: %s\n", err)
	}
	if Config.Debug {
		pinger.Debug = true
	}
	pinger.SetPrivileged(Config.Privileged)

	for i := 0; i < target.BatchSize; i++ {

		pinger.Count = target.Probes

		err = pinger.Run() // blocks until finished
		if err != nil {
			log.Errorf("Pinger error: %s\n", err)
		}

		stats := pinger.Statistics() // get send/receive/rtt stats

		probes[i] = Probe{
			MinRTT:    stats.MinRtt.Nanoseconds() / 1000000,
			MaxRTT:    stats.MaxRtt.Nanoseconds() / 1000000,
			Median:    stats.AvgRtt.Nanoseconds() / 1000000,
			NumProbes: target.Probes,
			Timestamp: time.Now()}
	}

	response := ResponsePacket{
		SatelliteName: probeName,
		ProbeType:     target.ProbeType,
		TargetName:    target.Name,
		Probes:        probes,
	}

	return response
}

func (target *Target) probeHttp(probeName string) ResponsePacket {

	probes := make([]Probe, target.BatchSize)

	for i := 0; i < target.BatchSize; i++ {

		j := 0

		min := intsets.MaxInt
		max := 0
		avg := 0

		for {
			req, err := http.NewRequest("GET", target.Host, nil)
			if err != nil {
				log.Errorf("Error running http probe: %s", err)
			}
			// Create a httpstat powered context
			var result httpstat.Result
			ctx := httpstat.WithHTTPStat(req.Context(), &result)
			req = req.WithContext(ctx)
			// Send request by default HTTP client
			client := http.DefaultClient
			res, err := client.Do(req)
			if err != nil {
				log.Errorf("Error running http probe: %s", err)
				break
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

			j++
			if j == target.Probes {
				avg = avg / target.Probes
				break
			}
		}

		probes[i] = Probe{
			MinRTT:    int64(min),
			MaxRTT:    int64(max),
			Median:    int64(avg),
			NumProbes: target.Probes,
			Timestamp: time.Now()}
	}

	response := ResponsePacket{
		SatelliteName: probeName,
		ProbeType:     target.ProbeType,
		TargetName:    target.Name,
		Probes:        probes,
	}

	return response
}

/**
func runExternalProbe(host string, probes int, probe string) ResponsePacket {

	r := ResponsePacket{MinRTT: int64(0),
		MaxRTT:    int64(0),
		Median:    int64(0),
		NumProbes: probes}

	return r
}
*/

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

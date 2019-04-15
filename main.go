package main

import "C"
import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/sparrc/go-ping"
	"github.com/spf13/viper"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"
)

type Configuration struct {
	Probes []Probe `mapstructure:"probes"`
	Targets map[string]Target `mapstructure:"targets"`
}

type Probe struct {
	Name string `mapstructure:"name"`
	Secret string `mapstructure:"secret"`
	Targets []string `mapstructure:"targets"`
}

type Target struct {
	Name string `mapstructur:"name"`
	Host string `mapstructure:"host"`
	ProbeType string `mapstructure:"probe_type"`
	Probes int `mapstructure:"probes"`
	Intervall int `mapstructure:"intervall"`
}

type ResponsePacket struct {
      ProbeName string
      TargetName string
      ProbeType string
      MinRTT int64
      MaxRTT int64
      Median int64
      NumProbes int
}

var Config Configuration

func main() {

	configPtr 	:= flag.String("config", "config", "config file")
	modePtr 	:= flag.String("mode", "master", "slave / master")
	masterPtr 	:= flag.String("master", "", "fqdn / ip of master server")
	probeNamePtr := flag.String("name", "", "name of probe")

	flag.Parse()

	fmt.Println("config:", *configPtr)
	fmt.Println("mode:", *modePtr)

	if (*modePtr == "master") {

		viper.Set("Verbose", true)
		viper.SetConfigName(*configPtr) // name of config file (without extension)
		viper.AddConfigPath("./config")               // optionally look for config in the working directory
		viper.SetConfigType("json")
		err := viper.ReadInConfig() // Find and read the config file

		if err != nil { // Handle errors reading the config file
			panic(fmt.Errorf("Fatal error config file: %s \n", err))
		}

		fmt.Printf("Using config: %s\n", viper.ConfigFileUsed())

		err = viper.Unmarshal(&Config)
		if err != nil {
			log.Fatalf("unable to decode into struct, %v", err)
		}

		fmt.Printf("%+v", Config)

		router := mux.NewRouter()
		router.HandleFunc("/probes/{name}", GetProbe).Methods("GET")
		router.HandleFunc("/targets/{name}", SubmitTarget).Methods("POST")
		log.Fatal(http.ListenAndServe(":8000", router))
	} else {
		request, _ := http.NewRequest("GET", *masterPtr + "probes/" + *probeNamePtr,nil)
		request.Header.Set("X-Authorisation", os.Getenv("NPROBE_SECRET"))
		client := &http.Client{}
		response, err := client.Do(request)
		if err != nil {
			fmt.Printf("The HTTP request failed with error %s\n", err)
		} else {
			data, _ := ioutil.ReadAll(response.Body)
			var targets []Target
			err := json.Unmarshal(data, &targets)

			log.Printf("Received targets: %+v", targets)

			if err != nil {
				log.Fatalf("unable to decode into struct, %v", err)
			}

			for {
				for _, k := range targets {
					go HandleProbe(k, *masterPtr, *probeNamePtr)
				}
				time.Sleep(60 * time.Second)
			}
		}
	}

}

func HandleProbe(k Target, master string, probeName string) {
	for {
		if k.ProbeType == "icmp" {
			r := probeIcmp(k.Host, k.Probes)
			r.TargetName = k.Name
			r.ProbeType = k.ProbeType
			r.ProbeName = probeName

			url := master + "targets/" + k.Name

			log.Printf("url: %s", url)

			jsonValue, _ := json.Marshal(r)
			request2, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonValue))
			request2.Header.Set("X-Authorisation", os.Getenv("PROBE_SECRET"))
			client2 := &http.Client{}
			body, err := client2.Do(request2)
			if err != nil {
				fmt.Printf("The HTTP request failed with error %s\n", err)
			}

			log.Printf("%+v", body)
		}
		time.Sleep(((time.Duration)k.Intervall * time.Second))
	}
}

func GetProbe(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	for _, probe := range Config.Probes  {
		if probe.Name == params["name"] && r.Header.Get("X-Authorisation") == probe.Secret {

			var targets []Target = make([]Target, len(probe.Targets))

			var i = 0;

			for _, k  := range probe.Targets {
				targets[i] = Config.Targets[k]
				i++
			}

			json.NewEncoder(w).Encode(targets)
			return
		}
	}
}


func SubmitTarget(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)

	log.Printf("%+v", params)

	var responsePacket ResponsePacket
	_ = json.NewDecoder(r.Body).Decode(&responsePacket)

	log.Printf("%+v", responsePacket)
}

func probeIcmp(hostname string, probes int) ResponsePacket {
	pinger, err := ping.NewPinger(hostname)
	if err != nil {
		panic(err)
	}
	pinger.Count = probes
	pinger.Run() // blocks until finished
	stats := pinger.Statistics() // get send/receive/rtt stats

	r := ResponsePacket{MinRTT: stats.MinRtt.Nanoseconds(),
						MaxRTT: stats.MaxRtt.Nanoseconds(),
						Median: stats.AvgRtt.Nanoseconds(),
						NumProbes: probes}

	return r
}

/*

func probeHttp(url string, probes int) {

}
 */
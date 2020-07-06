package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/sparrc/go-ping"
	"github.com/spf13/viper"
	"github.com/tcnksm/go-httpstat"
	"golang.org/x/tools/container/intsets"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"
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
				fmt.Printf("Unmarshal error: %s", err)
			}

			var wg sync.WaitGroup

			for _, k := range targets {
				wg.Add(1)
				go HandleProbe(k, *masterPtr, *probeNamePtr, wg)
			}
			wg.Wait()
		}
	}

}

func HandleProbe(k Target, master string, probeName string, wg sync.WaitGroup) {
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

		url := master + "targets/" + k.Name

		jsonValue, _ := json.Marshal(r)
		request2, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonValue))
		request2.Header.Set("X-Authorisation", os.Getenv("PROBE_SECRET"))
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
		fmt.Printf("Pinger error: %s\n", err)
	}
	pinger.Count = probes
	pinger.Run() // blocks until finished
	stats := pinger.Statistics() // get send/receive/rtt stats

	r := ResponsePacket{MinRTT: stats.MinRtt.Nanoseconds()/1000000,
						MaxRTT: stats.MaxRtt.Nanoseconds()/1000000,
						Median: stats.AvgRtt.Nanoseconds()/1000000,
						NumProbes: probes}

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

		con := int(result.TCPConnection/time.Millisecond)

		if con < min {
			min = con
		}
		if con > max {
			max = con
		}
		avg+= con

		i++
		if (i == probes) {
			avg= avg/probes
			break
		}
	}

	r := ResponsePacket{MinRTT: int64(min),
						MaxRTT: int64(max),
						Median: int64(avg),
						NumProbes: probes}

	return r
}

func runExternalProbe(host string, probes int, probe string) ResponsePacket {


	r := ResponsePacket{MinRTT: int64(0),
						MaxRTT: int64(0),
						Median: int64(0),
						NumProbes: probes}

	return r
}

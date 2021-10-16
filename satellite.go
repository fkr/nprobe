package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-ping/ping"
	"github.com/tcnksm/go-httpstat"
	"golang.org/x/tools/container/intsets"
	log "github.com/sirupsen/logrus"
)

func HandleProbe(k Target, headUrl string, probeName string, wg *sync.WaitGroup) {
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

		url := headUrl + "targets/" + k.Name

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



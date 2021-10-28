package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"time"

	"github.com/digitaljanitors/go-httpstat"
	"github.com/go-ping/ping"
	log "github.com/sirupsen/logrus"
)

func (wk *Worker) HandleProbe(ch chan *Worker) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if err, ok := r.(error); ok {
				wk.Err = err
			} else {
				wk.Err = fmt.Errorf("Panic happened with %v", r)
			}
		} else {
			wk.Err = err
		}
		ch <- wk
	}()

	for {
		var r = ResponsePacket{}

		if wk.Target.ProbeType == "icmp" {
			r = wk.Target.probeIcmp(wk.ProbeName)
		}
		if wk.Target.ProbeType == "http" {
			r = wk.Target.probeHttp(wk.ProbeName)
		}

		url := wk.HeadUrl + "targets/" + wk.Target.Name

		jsonValue, _ := json.Marshal(r)
		request2, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonValue))
		request2.Header.Set("X-Authorization", os.Getenv("NPROBE_SECRET"))
		client2 := &http.Client{}
		body, err := client2.Do(request2)
		if err != nil {
			log.Errorf("The HTTP request failed with error %s\n", err)
		}

		log.Debugf("%+v", body)
	}
}

func (target *Target) probeIcmp(probeName string) ResponsePacket {

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
			MinRTT:    float64(stats.MinRtt.Nanoseconds()) / 1000000,
			MaxRTT:    float64(stats.MaxRtt.Nanoseconds()) / 1000000,
			Median:    float64(stats.AvgRtt.Nanoseconds()) / 1000000,
			Loss:	   stats.PacketLoss,
			NumProbes: target.Probes,
			Timestamp: time.Now()}

		log.Debugf("Probe '%s' of type '%s': Min: %f, Max: %f, Median: %f", target.Name, target.ProbeType,
					probes[i].MinRTT, probes[i].MaxRTT, probes[i].Median)
		log.Debugf("Probe '%s' of type '%s' sleeping for %d", target.Name, target.ProbeType, target.Interval)
		time.Sleep(time.Duration(target.Interval) * time.Second)
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

		min := math.MaxFloat64
		max := 0.0

		for j := 0; j < target.Probes; j++ {
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
			result.End(time.Now())
			err = res.Body.Close()

			if err != nil {
				log.Errorf("Error closing http request: %s", err)
			}

			con := float64(result.Total) / float64(time.Millisecond)
			log.Debugf("%s: %+v\n", target.Name, result)

			if con < min {
				min = con
			}
			if con > max {
				max = con
			}
		}

		probes[i] = Probe{
			MinRTT:    min,
			MaxRTT:    max,
			Median:    (min+max)/2,
			NumProbes: target.Probes,
			Timestamp: time.Now()}

		log.Debugf("Probe '%s' of type '%s' sleeping for %d", target.Name, target.ProbeType, target.Interval)
		time.Sleep(time.Duration(target.Interval) * time.Second)
	}

	response := ResponsePacket{
		SatelliteName: probeName,
		ProbeType:     target.ProbeType,
		TargetName:    target.Name,
		Probes:        probes,
	}

	return response
}

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"time"

	"github.com/digitaljanitors/go-httpstat"
	"github.com/go-ping/ping"
)

func (wk *Worker) HandleProbe(ch chan *Worker) (err error) {
	defer func() {
		log.WithFields(logrus.Fields{"worker": wk.Id, "target": wk.Target.Name}).Error("Running through defer")
		if r := recover(); r != nil {
			if err, ok := r.(error); ok {
				wk.Err = err
			} else {
				wk.Err = fmt.Errorf("Panic happened with %v", r)
				log.WithFields(logrus.Fields{"worker": wk.Id, "target": wk.Target.Name, "error": wk.Err}).Error("Paniced")
			}
		} else {
			wk.Err = err
			log.WithFields(logrus.Fields{"worker": wk.Id, "target": wk.Target.Name, "error": wk.Err}).Error("Error")
		}
		ch <- wk
	}()

	for {
		log.WithFields(logrus.Fields{
			"target":   wk.Target.Name,
			"type":     wk.Target.ProbeType,
			"interval": wk.Target.Interval,
		}).Debug("Sleeping in main for loop")
		time.Sleep(time.Duration(wk.Target.Interval) * time.Second)
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
		request2.Header.Set("X-Nprobe-Version", version)
		client2 := &http.Client{}
		body, err := client2.Do(request2)
		if err != nil {
			log.WithFields(logrus.Fields{"error": err}).Error("HTTP request failed")
		}

		log.WithFields(logrus.Fields{"body": body}).Debug()
	}
}

func (target *Target) probeIcmp(probeName string) ResponsePacket {

	probes := make([]Probe, target.BatchSize)
	pinger, err := ping.NewPinger(target.Host)
	if err != nil {
		log.WithFields(logrus.Fields{"error": err}).Error("Pinger error")
	}
	if Config.Debug {
		pinger.Debug = true
	}
	pinger.SetPrivileged(Config.Privileged)

	for i := 0; i < target.BatchSize; i++ {

		pinger.Count = target.Probes

		err = pinger.Run() // blocks until finished
		if err != nil {
			log.WithFields(logrus.Fields{"error": err}).Error("Pinger error")
		}

		stats := pinger.Statistics() // get send/receive/rtt stats

		probes[i] = Probe{
			MinRTT:    float64(stats.MinRtt.Nanoseconds()) / 1000000,
			MaxRTT:    float64(stats.MaxRtt.Nanoseconds()) / 1000000,
			Median:    float64(stats.AvgRtt.Nanoseconds()) / 1000000,
			Loss:	   stats.PacketLoss,
			NumProbes: target.Probes,
			Timestamp: time.Now()}

		log.WithFields(logrus.Fields{
			"target": target.Name,
			"type": target.ProbeType,
			"min": probes[i].MinRTT,
			"max": probes[i].MaxRTT,
			"median": probes[i].Median,
			"loss": probes[i].Loss,
		}).Debug()
		if i != 0 {
			log.WithFields(logrus.Fields{
				"target":   target.Name,
				"type":     target.ProbeType,
				"interval": target.Interval,
			}).Debug("Sleeping in probe loop")
			time.Sleep(time.Duration(target.Interval) * time.Second)
		}
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
				log.WithFields(logrus.Fields{"error": err}).Error("http probe error")
			}
			// Create a httpstat powered context
			var result httpstat.Result
			ctx := httpstat.WithHTTPStat(req.Context(), &result)
			req = req.WithContext(ctx)
			// Send request by default HTTP client
			client := http.DefaultClient
			res, err := client.Do(req)
			if err != nil {
				log.WithFields(logrus.Fields{"error": err}).Error("http probe error")
				break
			}
			if _, err := io.Copy(ioutil.Discard, res.Body); err != nil {
				log.WithFields(logrus.Fields{"error": err}).Fatal()
			}
			result.End(time.Now())
			err = res.Body.Close()

			if err != nil {
				log.WithFields(logrus.Fields{"error": err}).Error("Error closing http request")
			}

			con := float64(result.Total) / float64(time.Millisecond)
			log.WithFields(logrus.Fields{"target": target.Name, "result": result}).Debug()

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

		if i != 0 {
			log.WithFields(logrus.Fields{
				"target":   target.Name,
				"type":     target.ProbeType,
				"interval": target.Interval,
			}).Debug("Sleeping in probe loop")
			time.Sleep(time.Duration(target.Interval) * time.Second)
		}
	}

	response := ResponsePacket{
		SatelliteName: probeName,
		ProbeType:     target.ProbeType,
		TargetName:    target.Name,
		Probes:        probes,
	}

	return response
}

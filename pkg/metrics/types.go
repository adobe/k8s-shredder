/*
Copyright 2025 Adobe. All rights reserved.
This file is licensed to you under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License. You may obtain a copy
of the License at http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software distributed under
the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
OF ANY KIND, either express or implied. See the License for the specific language
governing permissions and limitations under the License.
*/

package metrics

import (
	"fmt"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Init ..
func Init(port int) error {
	if err := registerMetrics(); err != nil {
		return err
	}
	if err := serve(port); err != nil {
		return err
	}
	return nil
}

func registerMetrics() error {
	prometheus.MustRegister(ShredderAPIServerRequestsTotal)
	prometheus.MustRegister(ShredderAPIServerRequestsDurationSeconds)
	prometheus.MustRegister(ShredderLoopsTotal)
	prometheus.MustRegister(ShredderLoopsDurationSeconds)
	prometheus.MustRegister(ShredderProcessedNodesTotal)
	prometheus.MustRegister(ShredderProcessedPodsTotal)
	prometheus.MustRegister(ShredderErrorsTotal)
	prometheus.MustRegister(ShredderPodErrorsTotal)
	prometheus.MustRegister(ShredderNodeForceToEvictTime)
	prometheus.MustRegister(ShredderPodForceToEvictTime)

	return nil
}

func serve(port int) error {
	http.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		},
	))

	http.HandleFunc("/healthz", func(res http.ResponseWriter, req *http.Request) {
		res.WriteHeader(200)
		_, err := res.Write([]byte("OK"))
		if err != nil {
			log.Errorln("Error while replying to /healthz request:", err)
		}
	})

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		ReadHeaderTimeout: 3 * time.Second,
	}

	go func() {
		log.Fatal(server.ListenAndServe(), nil)
	}()
	return nil
}

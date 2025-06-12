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

package cmd

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/adobe/k8s-shredder/pkg/config"
	"github.com/adobe/k8s-shredder/pkg/handler"
	"github.com/adobe/k8s-shredder/pkg/metrics"
	"github.com/adobe/k8s-shredder/pkg/utils"
	"github.com/fsnotify/fsnotify"
	"github.com/go-co-op/gocron/v2"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile, logLevel, logFormat string
	dryRun                       bool
	metricsPort                  int
	cfg                          config.Config
	appContext                   *utils.AppContext
	scheduler                    gocron.Scheduler

	rootCmd = &cobra.Command{
		Use:              "k8s-shredder",
		Short:            "a novel way of dealing with kubernetes nodes blocked from draining",
		Long:             `a novel way of dealing with kubernetes nodes blocked from draining`,
		PersistentPreRun: preRun,
		Run:              run,
	}

	buildVersion = "dev"
	gitSHA       = "dev"
	buildTime    = "dev"
)

// Execute is the main function
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		log.Fatalln(err.Error())
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "The config file [yaml]")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Don't perform any actions, just log what happens")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", log.DebugLevel.String(), "The verbosity level of the logs, can be [panic|fatal|error|warn|warning|info|debug|trace]")
	rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", "text", "The output format of the logs, can be [text|json]")
	rootCmd.PersistentFlags().IntVar(&metricsPort, "metrics-port", 9999, "The port used by the metrics server")
	err := rootCmd.MarkPersistentFlagRequired("config")
	if err != nil {
		log.Fatalln("No config flag configured")
	}
}

func setupAppContext(cfg config.Config, dryRun bool) {
	var err error

	appContext, err = utils.NewAppContext(cfg, dryRun)

	if err != nil {
		log.Fatalln("Failed to setup application context: ", err)
	}
}

func setupLogging(logLevel, logFormat string) {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		level = log.DebugLevel
	}
	log.SetLevel(level)

	logFormat = strings.ToLower(logFormat)
	if logFormat == "json" {
		log.SetFormatter(&log.JSONFormatter{})
	} else {
		log.SetFormatter(&log.TextFormatter{})
	}
}

func setupMetricsServer() {
	log.Infoln("Initializing metrics server")

	err := metrics.Init(metricsPort)
	if err != nil {
		log.Fatalf("Failed to setup metric server: %s", err)
	}
}

func discoverConfig() {
	viper.SetConfigFile(cfgFile)
	// Set default values in case they are omitted in config file
	viper.SetDefault("EvictionLoopInterval", time.Second*60)
	viper.SetDefault("ParkedNodeTTL", time.Minute*60)
	viper.SetDefault("RollingRestartThreshold", 0.5)
	viper.SetDefault("UpgradeStatusLabel", "shredder.ethos.adobe.net/upgrade-status")
	viper.SetDefault("ExpiresOnLabel", "shredder.ethos.adobe.net/parked-node-expires-on")
	viper.SetDefault("NamespacePrefixSkipInitialEviction", "")
	viper.SetDefault("RestartedAtAnnotation", "shredder.ethos.adobe.net/restartedAt")
	viper.SetDefault("AllowEvictionLabel", "shredder.ethos.adobe.net/allow-eviction")
	viper.SetDefault("ToBeDeletedTaint", "ToBeDeletedByClusterAutoscaler")
	viper.SetDefault("ArgoRolloutsAPIVersion", "v1alpha1")
	viper.SetDefault("EnableKarpenterDriftDetection", false)
	viper.SetDefault("ParkedByLabel", "shredder.ethos.adobe.net/parked-by")
	viper.SetDefault("ParkedByValue", "k8s-shredder")
	viper.SetDefault("ParkedNodeTaint", "shredder.ethos.adobe.net/upgrade-status=parked:NoSchedule")
	viper.SetDefault("EnableNodeLabelDetection", false)
	viper.SetDefault("NodeLabelsToDetect", []string{})

	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalf("Failed to discover configuration file %s: %s", cfgFile, err)
	}
	viper.WatchConfig()
	viper.OnConfigChange(func(e fsnotify.Event) {
		log.Infof("Configuration file `%s` changed, attempting to reload", e.Name)
		reset()
		parseConfig()
		appContext.Config = cfg
		run(&cobra.Command{}, []string{})
	})
}

func parseConfig() {
	err := viper.Unmarshal(&cfg)
	if err != nil {
		log.Fatalf("Failed to parse configuration: %s", err)
	}
	log.WithFields(log.Fields{
		"EvictionLoopInterval":               cfg.EvictionLoopInterval.String(),
		"ParkedNodeTTL":                      cfg.ParkedNodeTTL.String(),
		"RollingRestartThreshold":            cfg.RollingRestartThreshold,
		"UpgradeStatusLabel":                 cfg.UpgradeStatusLabel,
		"ExpiresOnLabel":                     cfg.ExpiresOnLabel,
		"NamespacePrefixSkipInitialEviction": cfg.NamespacePrefixSkipInitialEviction,
		"RestartedAtAnnotation":              cfg.RestartedAtAnnotation,
		"AllowEvictionLabel":                 cfg.AllowEvictionLabel,
		"ToBeDeletedTaint":                   cfg.ToBeDeletedTaint,
		"ArgoRolloutsAPIVersion":             cfg.ArgoRolloutsAPIVersion,
		"EnableKarpenterDriftDetection":      cfg.EnableKarpenterDriftDetection,
		"ParkedByLabel":                      cfg.ParkedByLabel,
		"ParkedByValue":                      cfg.ParkedByValue,
		"ParkedNodeTaint":                    cfg.ParkedNodeTaint,
		"EnableNodeLabelDetection":           cfg.EnableNodeLabelDetection,
		"NodeLabelsToDetect":                 cfg.NodeLabelsToDetect,
	}).Info("Loaded configuration")
}

func preRun(cmd *cobra.Command, args []string) {
	setupLogging(logLevel, logFormat)
	// APP Build information
	log.WithFields(
		log.Fields{
			"Version":   buildVersion,
			"GitSHA":    gitSHA,
			"BuildTime": buildTime,
		}).Infoln("K8s-shredder info")

	setupMetricsServer()
	discoverConfig()
	parseConfig()
	setupAppContext(cfg, dryRun)
}

func run(cmd *cobra.Command, args []string) {
	var err error
	scheduler, err = gocron.NewScheduler(gocron.WithLocation(time.UTC))
	defer func() { _ = scheduler.Shutdown() }()

	if err != nil {
		log.Fatalf("Failed to create scheduler: %s", err)
	}

	h := handler.NewHandler(appContext)

	job, err := scheduler.NewJob(
		gocron.DurationJob(
			cfg.EvictionLoopInterval,
		),
		gocron.NewTask(
			h.Run,
		),
	)

	if err != nil {
		log.Fatalf("Failed to configure scheduler's job: %s", err)
	}

	// each job has a unique id
	log.Infof("Configured scheduler job with ID: %s", job.ID())

	activeJobs := make([]uuid.UUID, 0)
	for _, j := range scheduler.Jobs() {
		activeJobs = append(activeJobs, j.ID())
	}
	log.Infoln("Active jobs:", activeJobs)

	scheduler.Start()
	log.Info("Scheduler started, happy shredding!")
	select {}
}

func reset() {
	// clear all running jobs and stop the scheduler
	err := scheduler.StopJobs()
	if err != nil {
		log.Errorf("Failed to stop running jobs: %s", err)
	}
	err = scheduler.Shutdown()
	if err != nil {
		log.Errorf("Failed to shutdown scheduler: %s", err)
	}
}

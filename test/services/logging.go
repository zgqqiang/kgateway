//go:build ignore

package services

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/onsi/gomega"
	errors "github.com/rotisserie/eris"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/kgateway-dev/kgateway/v2/test/testutils"
)

const (
	// _loggingConfigRegex is the format of the string that can be passed to configure the log level of services
	// It is currently unused, but is here for reference
	// In general, we try to use the name of the deployment, e.g. gateway-proxy, gloo, discovery, etc.
	// for the name of the service. To confirm the name of the service that is being used, check the
	// invocation for the given service
	_loggingConfigRegex = "serviceA:logLevel,serviceB:logLevel"
	pairSeparator       = ","
	nameValueSeparator  = ":"
)

var (
	logProviderSingleton = &logProvider{
		defaultLogLevel: zapcore.InfoLevel,
		serviceLogLevel: make(map[string]zapcore.Level, 3),
	}
)

func init() {
	// Initialize the log provider with the log level provided in the environment variable
	LoadUserDefinedLogLevelFromEnv()
}

// LoadUserDefinedLogLevelFromEnv loads the log level from the environment variable
// and resets the entire LogProvider state to the values in that environment variable
func LoadUserDefinedLogLevelFromEnv() {
	logProviderSingleton.ReloadFromEnv()
}

// LoadUserDefinedLogLevel loads the log level from the provided string
// and resets the entire LogProvider state to the values in that string
func LoadUserDefinedLogLevel(userDefinedLogLevel string) {
	logProviderSingleton.ReloadFromString(userDefinedLogLevel)
}

// GetLogLevel returns the log level for the given service
func GetLogLevel(serviceName string) zapcore.Level {
	return logProviderSingleton.GetLogLevel(serviceName)
}

// SetLogLevel sets the log level for the given service
func SetLogLevel(serviceName string, logLevel zapcore.Level) {
	logProviderSingleton.SetLogLevel(serviceName, logLevel)
}

// IsDebugLogLevel returns true if the given service is logging at the debug level
func IsDebugLogLevel(serviceName string) bool {
	logLevel := GetLogLevel(serviceName)
	return logLevel == zapcore.DebugLevel
}

// MustGetSugaredLogger returns a sugared logger for the given service
// This logger is configured with the appropriate log level
func MustGetSugaredLogger(serviceName string) *zap.SugaredLogger {
	logLevel := GetLogLevel(serviceName)

	config := zap.NewProductionConfig()
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.Level.SetLevel(logLevel)

	logger, err := config.Build()
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to build logger for: %s", serviceName)

	return logger.Sugar()
}

// logProvider is a helper for managing the log level of multiple services
type logProvider struct {
	sync.RWMutex

	defaultLogLevel zapcore.Level
	serviceLogLevel map[string]zapcore.Level
}

func (l *logProvider) GetLogLevel(serviceName string) zapcore.Level {
	l.RLock()
	defer l.RUnlock()

	logLevel, ok := l.serviceLogLevel[serviceName]
	if !ok {
		return l.defaultLogLevel
	}
	return logLevel
}

func (l *logProvider) SetLogLevel(serviceName string, logLevel zapcore.Level) {
	l.Lock()
	defer l.Unlock()

	l.serviceLogLevel[serviceName] = logLevel
}

func (l *logProvider) ReloadFromEnv() {
	l.ReloadFromString(os.Getenv(testutils.ServiceLogLevel))
}

func (l *logProvider) ReloadFromString(userDefinedLogLevel string) {
	l.Lock()
	defer l.Unlock()

	serviceLogPairs := strings.Split(userDefinedLogLevel, pairSeparator)
	serviceNameSet := sets.NewString()
	for _, serviceLogPair := range serviceLogPairs {
		nameValue := strings.Split(serviceLogPair, nameValueSeparator)
		if len(nameValue) != 2 {
			continue
		}

		serviceName := nameValue[0]
		logLevelStr := nameValue[1]

		if serviceNameSet.Has(serviceName) {
			// This isn't an error, but we want to warn the user that with multiple definitions
			// there may be unknown behavior
			fmt.Printf("WARNING: duplicate service name found in log level string: %s\n", serviceName)
		}

		logLevel, err := zapcore.ParseLevel(logLevelStr)
		// We intentionally error loudly here
		// This will occur if the user passes an invalid log level string
		if err != nil {
			panic(errors.Wrapf(err, "invalid log level string: %s", logLevelStr))
		}

		// This whole function operates with a lock, so we can modify the map directly
		logProviderSingleton.serviceLogLevel[serviceName] = logLevel
		serviceNameSet.Insert(serviceName)
	}
}

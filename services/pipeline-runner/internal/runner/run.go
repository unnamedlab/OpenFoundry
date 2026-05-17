package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/openfoundry/openfoundry-go/services/pipeline-runner/internal/server"
)

// envHealthAddr names the env var that overrides the default
// /healthz + /metrics bind address. PORT is honored as a fallback to
// match the k8s downward API convention used by sibling sinks.
const (
	envHealthAddr     = "OF_PIPELINE_RUNNER_HEALTH_ADDR"
	envHealthPort     = "PORT"
	defaultHealthAddr = "0.0.0.0:9090"
)

// Spark integration mode. Override with OF_PIPELINE_RUNNER_SPARK_MODE
// in the SparkApplication spec when integration tests need a
// hermetic stub. Production always uses "spark-submit".
const (
	envSparkMode    = "OF_PIPELINE_RUNNER_SPARK_MODE"
	envSparkSubmit  = "OF_PIPELINE_RUNNER_SPARK_SUBMIT"
	envSparkAppJar  = "OF_PIPELINE_RUNNER_SPARK_JAR"
	envSparkMain    = "OF_PIPELINE_RUNNER_SPARK_MAIN_CLASS"
	envExtraConfArg = "OF_PIPELINE_RUNNER_EXTRA_CONF"
)

// sparkSubmitDefault matches the upstream apache/spark layout.
const (
	sparkSubmitDefault    = "/opt/spark/bin/spark-submit"
	sparkAppJarDefault    = "/opt/spark/jars/pipeline-runner-spark.jar"
	sparkMainClassDefault = "com.openfoundry.pipeline.PipelineRunner"
)

// Run drives the entire orchestration: argument logging, spec
// resolution, and Spark submission.
//
// Spark itself has no Go runtime; the actual `df.writeTo(...).append()`
// pathway lives in the Scala JAR shipped alongside this binary. Go's
// job is to materialise the resolved SQL into a JSON file that the
// Scala main reads from /tmp/spec.json — a much smaller surface than
// re-implementing Spark's catalog handshake.
//
// `OF_PIPELINE_RUNNER_SPARK_MODE=stub` short-circuits the fork and
// just prints the resolved spec; that mode is what integration tests
// run with so CI does not need a full Spark image.
func Run(args Args) error {
	log(args, fmt.Sprintf("starting (smoke=%t, version=%s)", args.Smoke, args.Version))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	addr := healthAddr()
	httpSrv := server.New(addr, "pipeline-runner", args.Version)
	log(args, fmt.Sprintf("health/metrics listening on %s", addr))

	var wg sync.WaitGroup
	httpErr := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := httpSrv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			httpErr <- err
		}
		close(httpErr)
	}()

	runErr := runWork(ctx, args)
	cancel()
	wg.Wait()

	if runErr != nil {
		return runErr
	}
	if err, ok := <-httpErr; ok && err != nil {
		return fmt.Errorf("health server: %w", err)
	}
	return nil
}

func runWork(ctx context.Context, args Args) error {
	spec, err := ResolveSpec(ctx, args)
	if err != nil {
		return err
	}
	log(args, fmt.Sprintf(
		"resolved transform: format=%s sql=%s",
		spec.Format, preview(spec.SQL),
	))

	switch strings.ToLower(strings.TrimSpace(os.Getenv(envSparkMode))) {
	case "", "spark-submit":
		return submitToSpark(ctx, args, spec)
	case "stub":
		log(args, "stub mode: skipping spark-submit (resolved spec dumped above)")
		return nil
	default:
		return fmt.Errorf("unknown %s value: %q", envSparkMode, os.Getenv(envSparkMode))
	}
}

// healthAddr resolves the bind address for /healthz + /metrics from
// OF_PIPELINE_RUNNER_HEALTH_ADDR (full host:port) or PORT (port only),
// falling back to 0.0.0.0:9090 — matches the sink services.
func healthAddr() string {
	if v := strings.TrimSpace(os.Getenv(envHealthAddr)); v != "" {
		return v
	}
	if p := strings.TrimSpace(os.Getenv(envHealthPort)); p != "" {
		return "0.0.0.0:" + p
	}
	return defaultHealthAddr
}

func submitToSpark(ctx context.Context, args Args, spec TransformSpec) error {
	submit := envOrDefault(envSparkSubmit, sparkSubmitDefault)
	jar := envOrDefault(envSparkAppJar, sparkAppJarDefault)
	mainClass := envOrDefault(envSparkMain, sparkMainClassDefault)

	cmd := exec.CommandContext(ctx, submit,
		"--class", mainClass,
		"--conf", "spark.sql.catalog."+args.Catalog+"=org.apache.iceberg.spark.SparkCatalog",
		"--conf", "spark.sql.catalog."+args.Catalog+".type=rest",
		"--conf", "spark.sql.catalog."+args.Catalog+".uri="+args.CatalogURI,
		"--conf", "spark.sql.extensions=org.apache.iceberg.spark.extensions.IcebergSparkSessionExtensions",
	)

	if extra := strings.TrimSpace(os.Getenv(envExtraConfArg)); extra != "" {
		for _, conf := range strings.Split(extra, " ") {
			conf = strings.TrimSpace(conf)
			if conf != "" {
				cmd.Args = append(cmd.Args, "--conf", conf)
			}
		}
	}

	cmd.Args = append(cmd.Args,
		jar,
		"--pipeline-id", args.PipelineID,
		"--run-id", args.RunID,
		"--input-dataset", args.InputDataset,
		"--output-dataset", args.OutputDataset,
		"--catalog", args.Catalog,
		"--catalog-uri", args.CatalogURI,
		"--inline-sql", spec.SQL,
		"--inline-format", spec.Format,
	)
	if args.Smoke {
		cmd.Args = append(cmd.Args, "--smoke")
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	log(args, fmt.Sprintf("invoking spark-submit: %s", submit))
	start := time.Now()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("spark-submit failed after %s: %w", time.Since(start).Round(time.Millisecond), err)
	}
	log(args, fmt.Sprintf("spark-submit exited cleanly in %s", time.Since(start).Round(time.Millisecond)))
	return nil
}

func envOrDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// preview is a one-line, length-capped rendering of an arbitrary
// SQL string for stdout. Spark Operator surfaces stdout via
// `kubectl logs`; the Foundry-style live log viewer pin-folds by
// pipeline_id / run_id, so we keep the prefix machine-parseable.
func preview(sql string) string {
	flat := strings.ReplaceAll(sql, "\n", " ")
	flat = strings.Join(strings.Fields(flat), " ")
	const max = 200
	if len(flat) <= max {
		return flat
	}
	return flat[:max] + "… (" + fmt.Sprintf("%d more", len(flat)-max) + ")"
}

func log(args Args, msg string) {
	fmt.Printf(
		"[pipeline-runner pipeline_id=%s run_id=%s] %s\n",
		args.PipelineID, args.RunID, msg,
	)
}

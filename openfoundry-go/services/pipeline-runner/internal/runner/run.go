package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
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
	sparkSubmitDefault   = "/opt/spark/bin/spark-submit"
	sparkAppJarDefault   = "/opt/spark/jars/pipeline-runner-spark.jar"
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

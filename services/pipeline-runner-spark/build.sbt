// pipeline-runner-spark — Spark/Iceberg JAR consumed by the Go pipeline-runner
// orchestrator. Bundles a single fat JAR with the OpenFoundry transform main
// class plus only project-local Scala code. Spark, Iceberg and Hadoop are
// "provided" — the Spark image baked into infra/helm/infra/spark-jobs already
// ships them.
ThisBuild / scalaVersion := "2.12.19"
ThisBuild / version      := sys.env.getOrElse("VERSION", "dev")
ThisBuild / organization := "com.openfoundry.pipeline"

lazy val root = (project in file("."))
  .settings(
    name := "pipeline-runner-spark",
    libraryDependencies ++= Seq(
      "org.apache.spark"  %% "spark-sql"                       % "3.5.4" % Provided,
      "org.apache.iceberg" % "iceberg-spark-runtime-3.5_2.12"  % "1.5.2" % Provided,
      "org.apache.iceberg" % "iceberg-aws-bundle"              % "1.5.2" % Provided,
      "org.apache.hadoop"  % "hadoop-aws"                      % "3.3.4" % Provided,
      "com.github.scopt"  %% "scopt"                            % "4.1.0",
    ),

    // sbt-assembly: produce <name>-<version>-assembly.jar, hand-merge the
    // standard collisions Iceberg/Hadoop bring in. Anything declared
    // Provided is excluded automatically.
    Compile / mainClass := Some("com.openfoundry.pipeline.PipelineRunner"),
    assembly / mainClass := Some("com.openfoundry.pipeline.PipelineRunner"),
    assembly / assemblyJarName := s"pipeline-runner-spark-${version.value}.jar",
    assembly / assemblyMergeStrategy := {
      case PathList("META-INF", "services", _ @ _*)            => MergeStrategy.concat
      case PathList("META-INF", "MANIFEST.MF")                 => MergeStrategy.discard
      case PathList("META-INF", _ @ _*)                        => MergeStrategy.first
      case "module-info.class"                                  => MergeStrategy.discard
      case x if x.endsWith(".class")                            => MergeStrategy.first
      case x if x.endsWith(".properties")                       => MergeStrategy.first
      case _                                                    => MergeStrategy.first
    },
  )

// services/pipeline-runner/build.sbt
//
// FASE 3 / Tarea 3.3 of docs/architecture/migration-plan-foundry-pattern-orchestration.md.
//
// Tiny Scala module. Produces target/scala-2.12/pipeline-runner_2.12-0.1.0.jar,
// which is COPYed into /opt/spark/jars/ in the runtime image.
//
// Design constraint: NO third-party runtime dependencies. spark-sql is
// `Provided` because the runtime base image (apache/spark:3.5.4) already
// ships it on the classpath, and Iceberg + AWS bundle JARs are dropped
// into /opt/spark/jars/ by the Dockerfile. The HTTP client used to fetch
// the resolved transform spec from pipeline-build-service is Java 17's
// stdlib `java.net.http.HttpClient` (no sttp/http4s/akka-http).
//
// Version pins documented in services/pipeline-runner/README.md
// ("Version pins" section). Bumping any one of these requires bumping
// the matching dependency in the runtime Dockerfile.

ThisBuild / scalaVersion := "2.12.18"
ThisBuild / organization := "com.openfoundry"
ThisBuild / version      := "0.1.0"

// Spark 3.5 only ships _2.12 runtime jars; we explicitly cross-build
// against 2.12 to match.
val sparkVersion   = "3.5.4"

lazy val `pipeline-runner` = (project in file("."))
  .settings(
    name := "pipeline-runner",
    // Match the Java version of the runtime image (apache/spark:3.5.4-...-java17-...).
    javacOptions ++= Seq("-source", "17", "-target", "17"),
    scalacOptions ++= Seq(
      "-deprecation",
      "-feature",
      "-unchecked",
      "-Xfatal-warnings",
      "-encoding", "UTF-8",
      // Don't set -target/-release here: Scala 2.12's -target option
      // syntax has shifted across patch releases (2.12 wants `-target:jvm-1.8`,
      // 2.13+ wants `-target:17`), and the resulting JDK-8 bytecode runs
      // fine on the JDK-17 of the runtime image. javacOptions above
      // already pins the Java sources to 17.
    ),
    libraryDependencies ++= Seq(
      // Provided: present on /opt/spark/jars at runtime.
      "org.apache.spark" %% "spark-sql" % sparkVersion % Provided,
    ),
    // Don't generate -sources / -javadoc jars: nobody publishes this.
    Compile / packageDoc / publishArtifact := false,
    Compile / packageSrc / publishArtifact := false,
  )

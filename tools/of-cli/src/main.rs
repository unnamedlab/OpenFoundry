mod benchmark;
mod mock_provider;
mod openapi;
mod smoke;
mod vector;

use std::path::PathBuf;

use anyhow::Result;
use clap::{Parser, Subcommand};

#[derive(Parser)]
#[command(name = "of", about = "OpenFoundry CLI", version)]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Generate and validate OpenAPI specifications and SDKs.
    Docs(DocsArgs),
    /// Run smoke tests against a live environment.
    Smoke(SmokeArgs),
    /// Run benchmark scenarios against a live environment.
    Benchmark(BenchmarkArgs),
    /// Vector store operations (reindex, etc.).
    Vector(VectorArgs),
}

// ── docs ──────────────────────────────────────────────────────────────────────

#[derive(clap::Args)]
struct DocsArgs {
    #[command(subcommand)]
    command: DocsCommands,
}

#[derive(Subcommand)]
enum DocsCommands {
    /// Generate an OpenAPI spec from proto sources.
    GenerateOpenapi(GenerateOpenapiArgs),
    /// Validate the checked-in OpenAPI spec matches what would be generated.
    ValidateOpenapi(ValidateOpenapiArgs),
    /// Generate a TypeScript SDK from an OpenAPI spec.
    GenerateSdkTypescript(SdkArgs),
    /// Validate the checked-in TypeScript SDK matches what would be generated.
    ValidateSdkTypescript(SdkArgs),
    /// Generate a Python SDK from an OpenAPI spec.
    GenerateSdkPython(SdkArgs),
    /// Validate the checked-in Python SDK matches what would be generated.
    ValidateSdkPython(SdkArgs),
    /// Generate a Java SDK from an OpenAPI spec.
    GenerateSdkJava(SdkArgs),
    /// Validate the checked-in Java SDK matches what would be generated.
    ValidateSdkJava(SdkArgs),
}

#[derive(clap::Args)]
struct GenerateOpenapiArgs {
    /// Directory containing .proto source files.
    #[arg(short, long)]
    proto_dir: PathBuf,
    /// Output path for the generated OpenAPI JSON file.
    #[arg(short, long)]
    output: PathBuf,
}

#[derive(clap::Args)]
struct ValidateOpenapiArgs {
    /// Directory containing .proto source files.
    #[arg(short, long)]
    proto_dir: PathBuf,
    /// Path to the checked-in OpenAPI spec to validate against.
    #[arg(short, long)]
    expected: PathBuf,
}

#[derive(clap::Args)]
struct SdkArgs {
    /// Path to the OpenAPI spec JSON file.
    #[arg(short, long)]
    input: PathBuf,
    /// Directory where the SDK files will be written (or validated).
    #[arg(short, long)]
    output: PathBuf,
}

// ── smoke ─────────────────────────────────────────────────────────────────────

#[derive(clap::Args)]
struct SmokeArgs {
    /// Path to the smoke test scenario JSON file.
    #[arg(short, long)]
    scenario: PathBuf,
    /// Path where the smoke test report JSON will be written.
    #[arg(short, long)]
    output: PathBuf,
}

// ── benchmark ─────────────────────────────────────────────────────────────────

#[derive(clap::Args)]
struct BenchmarkArgs {
    /// Path to the benchmark scenario JSON file.
    #[arg(short, long)]
    scenario: PathBuf,
    /// Path where the benchmark report JSON will be written.
    #[arg(short, long)]
    output: PathBuf,
}

// ── vector ────────────────────────────────────────────────────────────────────

#[derive(clap::Args)]
struct VectorArgs {
    #[command(subcommand)]
    command: VectorCommands,
}

#[derive(Subcommand)]
enum VectorCommands {
    /// Reindex embeddings from one backend to another.
    Reindex(vector::ReindexArgs),
}

// ── entry point ───────────────────────────────────────────────────────────────

#[tokio::main]
async fn main() -> Result<()> {
    let cli = Cli::parse();
    match cli.command {
        Commands::Docs(DocsArgs { command }) => match command {
            DocsCommands::GenerateOpenapi(args) => {
                let spec = openapi::generate_spec(&args.proto_dir)?;
                let json = serde_json::to_string_pretty(&spec)?;
                std::fs::write(&args.output, json)?;
            }
            DocsCommands::ValidateOpenapi(args) => {
                openapi::validate_generated_spec(&args.proto_dir, &args.expected)?;
            }
            DocsCommands::GenerateSdkTypescript(args) => {
                openapi::generate_typescript_sdk(&args.input, &args.output)?;
            }
            DocsCommands::ValidateSdkTypescript(args) => {
                openapi::validate_typescript_sdk(&args.input, &args.output)?;
            }
            DocsCommands::GenerateSdkPython(args) => {
                openapi::generate_python_sdk(&args.input, &args.output)?;
            }
            DocsCommands::ValidateSdkPython(args) => {
                openapi::validate_python_sdk(&args.input, &args.output)?;
            }
            DocsCommands::GenerateSdkJava(args) => {
                openapi::generate_java_sdk(&args.input, &args.output)?;
            }
            DocsCommands::ValidateSdkJava(args) => {
                openapi::validate_java_sdk(&args.input, &args.output)?;
            }
        },
        Commands::Smoke(args) => {
            smoke::run_suite(&args.scenario, &args.output).await?;
        }
        Commands::Benchmark(args) => {
            benchmark::run_suite(&args.scenario, &args.output).await?;
        }
        Commands::Vector(VectorArgs {
            command: VectorCommands::Reindex(args),
        }) => {
            vector::run_reindex(&args).await?;
        }
    }
    Ok(())
}

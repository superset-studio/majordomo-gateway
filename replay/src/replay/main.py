"""Replay logged LLM requests against a target model and compare outputs."""

import argparse
import asyncio
import sys
from pathlib import Path

import yaml
from majordomo_llm import get_llm_instance
from rich.console import Console
from rich.progress import Progress

from replay.compare import compare
from replay.fetch import fetch_requests
from replay.report import ReplayResult, print_report


async def run(config: dict) -> None:
    console = Console()

    source_config = config.get("source", {})
    target_config = config.get("target", {})
    judge_config = config.get("judge", {})

    # Fetch logged requests
    console.print("[bold]Fetching logged requests...[/bold]")
    requests = await fetch_requests(config)

    if not requests:
        console.print("[yellow]No matching requests found.[/yellow]")
        return

    console.print(f"Found {len(requests)} requests to replay.\n")

    # Set up target LLM
    target_llm = get_llm_instance(
        provider=target_config["provider"],
        model=target_config["model"],
    )

    results: list[ReplayResult] = []

    with Progress(console=console) as progress:
        task = progress.add_task("Replaying requests...", total=len(requests))

        for req in requests:
            try:
                response = await target_llm.get_response(
                    user_prompt=req.user_prompt,
                    system_prompt=req.system_prompt,
                )

                comparison = await compare(
                    user_prompt=req.user_prompt,
                    original_response=req.original_response,
                    replay_response=response.content,
                    judge_config=judge_config,
                )

                results.append(
                    ReplayResult(
                        request=req,
                        replay_response=response.content,
                        replay_cost=response.total_cost,
                        replay_latency_ms=response.response_time * 1000,
                        replay_input_tokens=response.input_tokens,
                        replay_output_tokens=response.output_tokens,
                        comparison=comparison,
                    )
                )
            except Exception as e:
                console.print(f"[red]Error replaying request {req.id}: {e}[/red]")

            progress.advance(task)

    console.print()
    print_report(results, source_config, target_config)


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Replay logged LLM requests against a target model and compare outputs.",
    )
    parser.add_argument(
        "--config",
        type=str,
        default="replay.yaml",
        help="Path to config file (default: replay.yaml)",
    )
    args = parser.parse_args()

    config_path = Path(args.config)
    if not config_path.exists():
        print(f"Error: config file not found: {config_path}", file=sys.stderr)
        sys.exit(1)

    with open(config_path) as f:
        config = yaml.safe_load(f)

    asyncio.run(run(config))


if __name__ == "__main__":
    main()

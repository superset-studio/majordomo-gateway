"""Rich terminal report for replay results."""

from dataclasses import dataclass

from rich.console import Console
from rich.panel import Panel
from rich.table import Table

from replay.compare import ComparisonResult
from replay.fetch import LoggedRequest


@dataclass
class ReplayResult:
    request: LoggedRequest
    replay_response: str
    replay_cost: float
    replay_latency_ms: float
    replay_input_tokens: int
    replay_output_tokens: int
    comparison: ComparisonResult


def print_report(
    results: list[ReplayResult],
    source_config: dict,
    target_config: dict,
) -> None:
    console = Console()

    if not results:
        console.print("[yellow]No requests to report on.[/yellow]")
        return

    # Compute stats
    total_original_cost = sum(r.request.cost for r in results)
    total_replay_cost = sum(r.replay_cost for r in results)
    avg_original_cost = total_original_cost / len(results)
    avg_replay_cost = total_replay_cost / len(results)

    avg_original_latency = sum(r.request.latency_ms for r in results) / len(results)
    avg_replay_latency = sum(r.replay_latency_ms for r in results) / len(results)

    exact_matches = sum(1 for r in results if r.comparison.exact_match)
    judged = [r for r in results if r.comparison.judge_equivalent is not None and not r.comparison.exact_match]
    judge_equivalent = sum(1 for r in judged if r.comparison.judge_equivalent)
    divergent = [r for r in results if not r.comparison.exact_match and r.comparison.judge_equivalent is False]

    total = len(results)
    exact_pct = exact_matches / total * 100
    equiv_pct = (exact_matches + judge_equivalent) / total * 100
    divergent_pct = len(divergent) / total * 100

    cost_savings = total_original_cost - total_replay_cost
    cost_savings_pct = (cost_savings / total_original_cost * 100) if total_original_cost > 0 else 0

    latency_improvement = avg_original_latency - avg_replay_latency
    latency_improvement_pct = (latency_improvement / avg_original_latency * 100) if avg_original_latency > 0 else 0

    # Filters summary
    filters_str = ", ".join(f"{k}={v}" for k, v in source_config.get("filters", {}).items())

    # Summary panel
    summary_lines = [
        f"[bold]Source model:[/bold] {source_config.get('model', 'any')}",
        f"[bold]Target model:[/bold] {target_config.get('provider', '')} / {target_config.get('model', '')}",
        f"[bold]Filters:[/bold] {filters_str or 'none'}",
        f"[bold]Requests replayed:[/bold] {total}",
    ]
    console.print(Panel("\n".join(summary_lines), title="Replay Summary", border_style="blue"))

    # Cost comparison table
    cost_table = Table(title="Cost Comparison")
    cost_table.add_column("", style="bold")
    cost_table.add_column("Original", justify="right")
    cost_table.add_column("Replay", justify="right")
    cost_table.add_row("Total", f"${total_original_cost:.6f}", f"${total_replay_cost:.6f}")
    cost_table.add_row("Average", f"${avg_original_cost:.6f}", f"${avg_replay_cost:.6f}")
    console.print(cost_table)

    # Latency comparison table
    latency_table = Table(title="Latency Comparison")
    latency_table.add_column("", style="bold")
    latency_table.add_column("Original", justify="right")
    latency_table.add_column("Replay", justify="right")
    latency_table.add_row("Avg (ms)", f"{avg_original_latency:.0f}", f"{avg_replay_latency:.0f}")
    console.print(latency_table)

    # Match rates table
    rates_table = Table(title="Match Rates")
    rates_table.add_column("Metric", style="bold")
    rates_table.add_column("Value", justify="right")
    rates_table.add_row("Exact match", f"{exact_pct:.1f}%")
    rates_table.add_row("Equivalent (exact + judge)", f"{equiv_pct:.1f}%")
    rates_table.add_row("Divergent", f"{divergent_pct:.1f}%")
    console.print(rates_table)

    # Savings summary
    savings_lines = [
        f"[bold]Cost savings:[/bold] ${cost_savings:.6f} ({cost_savings_pct:.1f}%)",
        f"[bold]Latency improvement:[/bold] {latency_improvement:.0f} ms ({latency_improvement_pct:.1f}%)",
    ]
    console.print(Panel("\n".join(savings_lines), title="Savings", border_style="green"))

    # Divergent examples
    if divergent:
        div_table = Table(title="Divergent Examples", show_lines=True)
        div_table.add_column("Prompt", max_width=40)
        div_table.add_column("Original Response", max_width=35)
        div_table.add_column("Replay Response", max_width=35)
        div_table.add_column("Judge Reason", max_width=30)

        for r in divergent:
            div_table.add_row(
                _truncate(r.request.user_prompt, 120),
                _truncate(r.request.original_response, 100),
                _truncate(r.replay_response, 100),
                r.comparison.judge_reason or "",
            )

        console.print(div_table)


def _truncate(text: str, max_len: int) -> str:
    if len(text) <= max_len:
        return text
    return text[: max_len - 3] + "..."

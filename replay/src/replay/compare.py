"""Compare original and replayed responses using exact match and LLM-as-judge."""

from dataclasses import dataclass

from majordomo_llm import get_llm_instance
from pydantic import BaseModel


@dataclass
class ComparisonResult:
    exact_match: bool
    judge_equivalent: bool | None  # None if judge disabled
    judge_reason: str | None


class JudgeVerdict(BaseModel):
    equivalent: bool
    reason: str


async def compare(
    user_prompt: str,
    original_response: str,
    replay_response: str,
    judge_config: dict,
) -> ComparisonResult:
    """Compare two responses, optionally using an LLM judge."""
    exact = original_response.strip() == replay_response.strip()

    if exact or not judge_config.get("enabled", False):
        return ComparisonResult(
            exact_match=exact,
            judge_equivalent=True if exact else None,
            judge_reason=None,
        )

    judge_llm = get_llm_instance(
        provider=judge_config["provider"],
        model=judge_config["model"],
    )

    prompt = (
        "Given the following prompt and two responses from different models, "
        "determine if the responses are functionally equivalent — i.e., a user "
        "would get the same value from either.\n\n"
        f"Prompt: {user_prompt}\n\n"
        f"Response A: {original_response}\n\n"
        f"Response B: {replay_response}"
    )

    result = await judge_llm.get_structured_json_response(
        response_model=JudgeVerdict,
        user_prompt=prompt,
    )

    verdict: JudgeVerdict = result.content
    return ComparisonResult(
        exact_match=False,
        judge_equivalent=verdict.equivalent,
        judge_reason=verdict.reason,
    )

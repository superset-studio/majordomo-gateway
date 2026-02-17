"""Parse provider-specific request/response bodies into prompts."""

import json


def extract_prompts(
    request_body: str, response_body: str, provider: str
) -> tuple[str, str | None, str]:
    """Extract user_prompt, system_prompt, and original_response from raw bodies.

    Returns:
        (user_prompt, system_prompt, original_response)
    """
    req = json.loads(request_body)
    resp = json.loads(response_body)

    user_prompt, system_prompt = _parse_request(req, provider)
    original_response = _parse_response(resp, provider)

    return user_prompt, system_prompt, original_response


def _parse_request(req: dict, provider: str) -> tuple[str, str | None]:
    """Extract (user_prompt, system_prompt) from a request body."""
    if provider == "anthropic":
        return _parse_anthropic_request(req)
    elif provider == "gemini":
        return _parse_gemini_request(req)
    else:
        # OpenAI and compatible providers (deepseek, cohere, etc.)
        return _parse_openai_request(req)


def _parse_response(resp: dict, provider: str) -> str:
    """Extract response text from a response body."""
    if provider == "anthropic":
        return _parse_anthropic_response(resp)
    elif provider == "gemini":
        return _parse_gemini_response(resp)
    else:
        return _parse_openai_response(resp)


def _parse_openai_request(req: dict) -> tuple[str, str | None]:
    messages = req.get("messages", [])
    system_prompt = None
    user_prompt = ""

    for msg in messages:
        if msg.get("role") == "system":
            system_prompt = _extract_text_content(msg.get("content", ""))
    for msg in reversed(messages):
        if msg.get("role") == "user":
            user_prompt = _extract_text_content(msg.get("content", ""))
            break

    return user_prompt, system_prompt


def _parse_anthropic_request(req: dict) -> tuple[str, str | None]:
    system_prompt = None
    system_field = req.get("system")
    if isinstance(system_field, str):
        system_prompt = system_field
    elif isinstance(system_field, list) and system_field:
        # Array of content blocks — take first text block
        for block in system_field:
            if isinstance(block, dict) and block.get("type") == "text":
                system_prompt = block.get("text", "")
                break

    user_prompt = ""
    for msg in reversed(req.get("messages", [])):
        if msg.get("role") == "user":
            content = msg.get("content", "")
            if isinstance(content, str):
                user_prompt = content
            elif isinstance(content, list):
                for block in content:
                    if isinstance(block, dict) and block.get("type") == "text":
                        user_prompt = block.get("text", "")
                        break
            break

    return user_prompt, system_prompt


def _parse_gemini_request(req: dict) -> tuple[str, str | None]:
    system_prompt = None
    si = req.get("system_instruction")
    if isinstance(si, dict):
        parts = si.get("parts", [])
        if parts and isinstance(parts[0], dict):
            system_prompt = parts[0].get("text")

    user_prompt = ""
    for content in reversed(req.get("contents", [])):
        if content.get("role") == "user":
            parts = content.get("parts", [])
            if parts and isinstance(parts[0], dict):
                user_prompt = parts[0].get("text", "")
            break

    return user_prompt, system_prompt


def _parse_openai_response(resp: dict) -> str:
    choices = resp.get("choices", [])
    if choices:
        return choices[0].get("message", {}).get("content", "")
    return ""


def _parse_anthropic_response(resp: dict) -> str:
    content = resp.get("content", [])
    if content and isinstance(content[0], dict):
        return content[0].get("text", "")
    return ""


def _parse_gemini_response(resp: dict) -> str:
    candidates = resp.get("candidates", [])
    if candidates:
        parts = candidates[0].get("content", {}).get("parts", [])
        if parts and isinstance(parts[0], dict):
            return parts[0].get("text", "")
    return ""


def _extract_text_content(content: str | list | dict) -> str:
    """Handle content that may be a string or a list of content blocks."""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        for block in content:
            if isinstance(block, dict) and block.get("type") == "text":
                return block.get("text", "")
            if isinstance(block, str):
                return block
    return str(content)

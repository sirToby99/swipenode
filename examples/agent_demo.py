"""
SwipeNode + OpenAI Agent Demo
==============================

This script demonstrates how to use SwipeNode as a data-extraction tool
inside a Python-based AI agent pipeline:

  1. SwipeNode fetches a URL and returns clean, structured text (no browser needed).
  2. The extracted text is passed to an OpenAI chat completion as context.
  3. The model answers a user question grounded in the page data.

Prerequisites
-------------
  pip install openai

  # Build SwipeNode (once)
  cd /path/to/swipenode && go build -o swipenode .

Usage
-----
  export OPENAI_API_KEY="sk-..."
  python examples/agent_demo.py "https://example.com" "Summarise this page in 3 bullets"
"""

import os
import subprocess
import sys

from openai import OpenAI


def extract(url: str, swipenode_bin: str = "./swipenode") -> str:
    """Call SwipeNode to extract page data. Returns the extracted text."""
    result = subprocess.run(
        [swipenode_bin, "extract", "--url", url],
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError(
            f"SwipeNode failed (exit {result.returncode}): {result.stderr.strip()}"
        )
    return result.stdout.strip()


def ask(context: str, question: str, model: str = "gpt-4o") -> str:
    """Send the extracted context + a user question to the OpenAI API."""
    client = OpenAI()  # reads OPENAI_API_KEY from env
    response = client.chat.completions.create(
        model=model,
        messages=[
            {
                "role": "system",
                "content": (
                    "You are a helpful assistant. Use ONLY the provided context "
                    "to answer the user's question. If the context does not contain "
                    "enough information, say so."
                ),
            },
            {
                "role": "user",
                "content": f"Context:\n{context}\n\nQuestion: {question}",
            },
        ],
    )
    return response.choices[0].message.content


def main() -> None:
    if len(sys.argv) < 3:
        print("Usage: python agent_demo.py <URL> <QUESTION>")
        print('  e.g. python agent_demo.py "https://example.com" "What is this page about?"')
        sys.exit(1)

    url = sys.argv[1]
    question = sys.argv[2]

    if not os.environ.get("OPENAI_API_KEY"):
        print("Error: set the OPENAI_API_KEY environment variable first.")
        sys.exit(1)

    print(f"Extracting data from {url} ...")
    context = extract(url)
    print(f"Extracted {len(context)} characters. Querying LLM ...\n")

    answer = ask(context, question)
    print(answer)


if __name__ == "__main__":
    main()

# Example Weft agents (Runtime Protocol v1 server).
# Build from the repository root:  docker build -f agents/Dockerfile -t weft/agents:dev .
# Set --build-arg EXTRAS=langgraph to run the reviewer as a real LangGraph StateGraph.
FROM python:3.12-slim

ARG EXTRAS=""
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 PIP_NO_CACHE_DIR=1 PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PORT=8000 HOST=0.0.0.0

WORKDIR /opt/weft
COPY sdk/python /opt/weft/sdk
COPY agents /opt/weft/agents

RUN pip install /opt/weft/sdk${EXTRAS:+[$EXTRAS]} /opt/weft/agents \
 && useradd --system --uid 10001 --home-dir /nonexistent --shell /usr/sbin/nologin agent

USER 10001
EXPOSE 8000
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD ["python", "-c", "import os,urllib.request; urllib.request.urlopen(f'http://127.0.0.1:{os.environ.get(\"PORT\",\"8000\")}/v1/health', timeout=2)"]
CMD ["python", "-m", "weft_agents"]

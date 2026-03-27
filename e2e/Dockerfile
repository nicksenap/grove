FROM python:3.12-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
        git jq \
    && rm -rf /var/lib/apt/lists/*

COPY . /src/grove
WORKDIR /src/grove

RUN pip install --no-cache-dir .

# Set up repos and config, then run the test suite
ENTRYPOINT ["bash", "e2e/run.sh"]

import os

DB_HOST = os.getenv("DB_HOST", "localhost")
WORKERS = int(os.environ["WORKERS"])
BUCKET = os.environ.get("S3_BUCKET")

# SENTRY_DSN is never provided by any configuration file.
SENTRY_DSN = os.getenv("SENTRY_DSN")

CREATE USER IF NOT EXISTS grafana_reader
IDENTIFIED WITH plaintext_password BY 'grafana-reader-local-password';

GRANT SELECT ON vodcoach.* TO grafana_reader;

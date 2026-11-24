-- Runs once, on first start of an empty data volume.
-- Casdoor keeps its own database next to the application database.
CREATE ROLE casdoor LOGIN PASSWORD 'casdoor';
CREATE DATABASE casdoor OWNER casdoor;

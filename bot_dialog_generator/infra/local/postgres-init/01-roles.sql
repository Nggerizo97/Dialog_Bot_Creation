-- Local development only: roles for studio-api. Runs once, when the data volume is new.
-- bdg_app owns nothing and carries the table privileges granted by the migrations;
-- studio_api logs in and inherits them, so row-level security always applies.
CREATE ROLE bdg_app NOLOGIN;
CREATE ROLE studio_api LOGIN PASSWORD 'studio_api_local' IN ROLE bdg_app;

-- Gives a plain Postgres the roles and default privileges a Supabase project
-- has, so the migrations and the access rules can be tested locally and in
-- CI. Not for production: a real Supabase project already has all of this.

create role anon nologin;
create role authenticated nologin;
create role service_role nologin bypassrls;
create role authenticator login noinherit password 'authenticator';
grant anon, authenticated, service_role to authenticator;

grant usage on schema public to anon, authenticated, service_role;
-- Supabase grants the API roles everything on new objects in public by
-- default. The migration has to take that back explicitly, and this makes
-- sure the tests would notice if it did not.
alter default privileges in schema public grant all on tables    to anon, authenticated, service_role;
alter default privileges in schema public grant all on functions to anon, authenticated, service_role;
alter default privileges in schema public grant all on sequences to anon, authenticated, service_role;

module github.com/plutoframework/pluto-core/examples/shipments/postgres

go 1.25.5

require (
	github.com/jackc/pgx/v5 v5.11.0
	github.com/plutoframework/pluto-auth v0.4.1-0.20261101183409-cb5554622172
	github.com/plutoframework/pluto-core v0.1.0
	github.com/plutoframework/pluto-rbac v0.0.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/plutoframework/pluto-core/stores/postgres v0.1.0
	github.com/plutoframework/pluto-jobs v0.1.1
	github.com/plutoframework/pluto-jobs/stores/postgres v0.1.0
	github.com/plutoframework/pluto-rbac/identity v0.0.0
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)

// Unpublished candidate reference consumer; not a release dependency.
replace github.com/plutoframework/pluto-core => ../../..

replace github.com/plutoframework/pluto-rbac/identity => ../../../../pluto-rbac/identity

replace github.com/plutoframework/pluto-rbac => ../../../../pluto-rbac

replace github.com/plutoframework/pluto-auth => ../../../../pluto-auth

replace github.com/plutoframework/pluto-core/stores/postgres => ../../../stores/postgres

replace github.com/plutoframework/pluto-jobs => ../../../../pluto-jobs

replace github.com/plutoframework/pluto-jobs/stores/postgres => ../../../../pluto-jobs/stores/postgres

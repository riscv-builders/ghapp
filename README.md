# ghapp

The Github app service behind riscv-builders

## Design
There are 3 components are available, web, migrate, manager

* Web: handle all incoming web traffic, include Github callback and stats on riscv-builders
* Migrate: Database migration tools
* Manager: Distribute jobs to actual runner
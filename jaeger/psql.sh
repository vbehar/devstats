#!/bin/bash
function finish {
    sync_unlock.sh
}
if [ -z "$TRAP" ]
then
  sync_lock.sh || exit -1
  trap finish EXIT
  export TRAP=1
fi
set -o pipefail
> errors.txt
> run.log
GHA2DB_PROJECT=jaeger IDB_DB=jaeger PG_DB=jaeger GHA2DB_LOCAL=1 ./structure 2>>errors.txt | tee -a run.log || exit 1
GHA2DB_PROJECT=jaeger IDB_DB=jaeger PG_DB=jaeger GHA2DB_LOCAL=1 ./gha2db 2016-11-01 0 today now 'jaegertracing,uber/jaeger' 2>>errors.txt | tee -a run.log || exit 2
GHA2DB_PROJECT=jaeger IDB_DB=jaeger PG_DB=jaeger GHA2DB_LOCAL=1 GHA2DB_MGETC=y GHA2DB_SKIPTABLE=1 GHA2DB_INDEX=1 ./structure 2>>errors.txt | tee -a run.log || exit 3
GHA2DB_PROJECT=jaeger PG_DB=jaeger ./shared/setup_repo_groups.sh 2>>errors.txt | tee -a run.log || exit 4
GHA2DB_PROJECT=jaeger IDB_DB=jaeger PG_DB=jaeger ./shared/import_affs.sh 2>>errors.txt | tee -a run.log || exit 5
./jaeger/setup_scripts.sh 2>>errors.txt | tee -a run.log || exit 6
GHA2DB_PROJECT=jaeger PG_DB=jaeger ./shared/get_repos.sh 2>>errors.txt | tee -a run.log || exit 7
GHA2DB_PROJECT=jaeger PG_DB=jaeger GHA2DB_LOCAL=1 ./pdb_vars || exit 8
./devel/ro_user_grants.sh jaeger || exit 9

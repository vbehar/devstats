select
  org.login as org,
  repo.name as repo,
  repo.id as rid,
  min(created_at) as date_from,
  max(created_at) as date_to
from
  [githubarchive:month.201801],
  [githubarchive:year.2017],
  [githubarchive:year.2016],
  [githubarchive:year.2015],
  [githubarchive:year.2014]
where
  repo.id = (
    select
      repo.id
    from
      [githubarchive:month.201801]
    where
      repo.name = 'current_org/current_repo'
    group by
      repo.id
  )
group by
  org, repo, rid
order by
  date_from
;

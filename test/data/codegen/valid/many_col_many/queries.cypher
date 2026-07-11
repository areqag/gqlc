// name: PeopleByAgeAndLocale :many
MATCH (p:Person) WHERE p.age > $minAge AND p.locale = $locale RETURN p.name, p.age

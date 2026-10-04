// name: PersonKnowsEdges :many
MATCH (:Person)-[k:KNOWS]->(:Person) RETURN k

// name: CompanyKnowsEdges :many
MATCH (:Company)-[k:KNOWS]->(:Company) RETURN k

// name: People :many
MATCH (p:Person) RETURN p

// name: Companies :many
MATCH (c:Company) RETURN c

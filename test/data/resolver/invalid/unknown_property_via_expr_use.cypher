MATCH (p:Person) WHERE p.doesnt_exist = $x RETURN p.name

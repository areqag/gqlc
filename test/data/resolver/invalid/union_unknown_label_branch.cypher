MATCH (a:Person) RETURN a.name UNION MATCH (b:NotDeclared) RETURN b.name

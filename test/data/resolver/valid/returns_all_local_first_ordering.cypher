MATCH (a:Person), (b:Post) WITH a, b MATCH (b:Post), (a:Person) RETURN *

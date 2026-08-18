MATCH (p)-[w:WROTE]->(b:Book)
MATCH (b)-[s:SHELVED_IN]->(sh:Shelf)
MATCH (p)-[t:SPOKE_AT]->(v:Venue)
MATCH (v)-[l:LICENSED_BY]->(co:Council)
RETURN p

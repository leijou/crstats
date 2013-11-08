Comic Rank Stats Server
=======

This is a much simpler version of the old Comic Rank reader tracking code, re-written using Go for the webserver and redis as the storage engine.

Stats should be collected for each comic frequently and stored in a proper database. That destination database is responsible for recording the historical data.

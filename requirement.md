# Problem Statement

## data
Assume you are building a backend service and a database for a food delivery platform, with
the following 2 raw datasets:
-​ Restaurants data
File: restaurant_with_menu.json
This dataset contains a list of restaurants with their menus and prices, as well as their cash
balances. This cash balance is the amount of money the restaurants hold in their merchant
accounts on this platform. It increases by the respective dish price whenever a user purchases
a dish from them.
-​ Users data
File: users_with_purchase_history.json
This dataset contains a list of users with their transaction history and cash balances. This cash
balance is the amount of money the users hold in their wallets on this platform. It decreases by
the dish price whenever they purchase a dish.

These are all raw data, which means that you are allowed to process and transform the data,
before you load it into your database.

## objectives
Now you have to build an API server, with a backing relational database (MySQL /
PostgreSQL) that will allow a front-end client to navigate through that sea of data easily, and
intuitively. The front-end team will later use that documentation to build the front-end clients.
We'd much prefer you to use Go / Python as a bonus.

### goals
The operations the front-end team would need you to support are:
- List all restaurants that are open at a certain datetime
-​ List top y restaurants that have more or less than x number of dishes within a price
range, ranked alphabetically. More or less (than x) is a parameter that the API allows
the consumer to enter.
-​ Search for restaurants or dishes by name, ranked by relevance to search term
-​ Process a user purchasing a dish from a restaurant, handling all relevant data changes
in an atomic transaction. Do watch out for potential race conditions that can arise from
concurrent transactions!

## packaging
In your repository, you would need to document the API interface, the commands to run the
ETL (extract, transform and load) script that takes in the raw data sets as input, and outputs to
your database, and the command to set up your server and database. Min Unit Test, Docker
and API documentation would be handy.
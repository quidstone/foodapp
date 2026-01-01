from datetime import datetime, time as dt_time

import etl_load_data as etl


def test_parse_opening_hours_single_day():
    result = etl.parse_opening_hours("Mon 2 pm - 5 pm")
    assert result == [(1, dt_time(14, 0), dt_time(17, 0))]


def test_parse_opening_hours_multi_range_with_overnight():
    hours_str = "Mon - Weds 2:30 pm - 8 pm / Fri - Sat 10 pm - 2 am"
    result = etl.parse_opening_hours(hours_str)
    assert result == [
        (1, dt_time(14, 30), dt_time(20, 0)),
        (2, dt_time(14, 30), dt_time(20, 0)),
        (3, dt_time(14, 30), dt_time(20, 0)),
        (5, dt_time(22, 0), dt_time(23, 59, 59)),
        (6, dt_time(0, 0), dt_time(2, 0)),
    ]

def test_parse_opening_hours_empty_string():
    assert etl.parse_opening_hours("") == []


def test_parse_transaction_datetime_valid_and_invalid():
    valid = etl.parse_transaction_datetime("02/10/2020 04:09 AM")
    assert valid == datetime(2020, 2, 10, 4, 9)
    assert etl.parse_transaction_datetime("bad date") is None


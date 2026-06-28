-- World Cup 2026 Round of 32 (1/16) matchups. Teams stored in canonical English
-- (UI localizes to RU). Idempotent: UPDATE by id, safe to re-run.
BEGIN;
UPDATE matches SET home='South Africa',  away='Canada'                 WHERE id=73; -- ЮАР – Канада
UPDATE matches SET home='Germany',       away='Paraguay'               WHERE id=74; -- Германия – Парагвай
UPDATE matches SET home='Netherlands',   away='Morocco'                WHERE id=75; -- Нидерланды – Марокко
UPDATE matches SET home='Brazil',        away='Japan'                  WHERE id=76; -- Бразилия – Япония
UPDATE matches SET home='France',        away='Sweden'                 WHERE id=77; -- Франция – Швеция
UPDATE matches SET home='Ivory Coast',   away='Norway'                 WHERE id=78; -- Кот-д'Ивуар – Норвегия
UPDATE matches SET home='Mexico',        away='Ecuador'                WHERE id=79; -- Мексика – Эквадор
UPDATE matches SET home='England',       away='DR Congo'               WHERE id=80; -- Англия – ДР Конго
UPDATE matches SET home='United States', away='Bosnia and Herzegovina' WHERE id=81; -- США – Босния и Герцеговина
UPDATE matches SET home='Belgium',       away='Senegal'                WHERE id=82; -- Бельгия – Сенегал
UPDATE matches SET home='Portugal',      away='Croatia'                WHERE id=83; -- Португалия – Хорватия
UPDATE matches SET home='Spain',         away='Austria'                WHERE id=84; -- Испания – Австрия
UPDATE matches SET home='Switzerland',   away='Algeria'                WHERE id=85; -- Швейцария – Алжир
UPDATE matches SET home='Argentina',     away='Cape Verde'             WHERE id=86; -- Аргентина – Кабо-Верде
UPDATE matches SET home='Colombia',      away='Ghana'                  WHERE id=87; -- Колумбия – Гана
UPDATE matches SET home='Australia',     away='Egypt'                  WHERE id=88; -- Австралия – Египет
COMMIT;

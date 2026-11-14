-- +goose Up
ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users
    ADD CONSTRAINT users_role_check CHECK (role IN ('platform_admin','org_owner','org_admin','project_admin','developer','viewer'));
ALTER TABLE model_candidates DROP COLUMN endpoint_id;
DROP TABLE model_endpoints;
-- 模型服务可以不绑定路由策略(走内置默认策略),此时决策记录没有显式策略 ID。
ALTER TABLE policy_decisions ALTER COLUMN route_policy_id DROP NOT NULL;
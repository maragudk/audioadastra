-- Insert an admin user
insert into users (id, did, active)
values ('u_f4958e9cd27a553b08092c790ea44fbb', 'did:plc:admin000000000000000000', 1);

-- Assign admin role to the user
insert into users_roles (user_id, role) values ('u_f4958e9cd27a553b08092c790ea44fbb', 'admin');

import { useCallback, useState } from "react";
import { api } from "./api";
import { ErrorMessage } from "./components";
import { DirectoryNavigation, DirectorySearch } from "./DirectoryControls";
import type { DirectoryFilter, Role } from "./types";
import { useDirectory } from "./useDirectory";

type Props = {
  tenant: string;
  role: Role;
  revision: number;
  canRead: boolean;
  canAssign: boolean;
  busy: boolean;
  onChange: (member: string, assign: boolean) => Promise<string>;
};

export function RoleMembers(props: Props) {
  const [assignment, setAssignment] = useState<"assigned" | "available">(
    "assigned",
  );
  return (
    <section className="role-members">
      <h3>Role members</h3>
      {props.canRead ? (
        <>
          <div
            className="button-row"
            role="group"
            aria-label="Role member view"
          >
            <button
              className="secondary-button"
              disabled={props.busy}
              aria-pressed={assignment === "assigned"}
              onClick={() => setAssignment("assigned")}
            >
              Assigned members
            </button>
            <button
              className="secondary-button"
              disabled={props.busy}
              aria-pressed={assignment === "available"}
              onClick={() => setAssignment("available")}
            >
              Available members
            </button>
          </div>
          <RoleMemberList key={assignment} {...props} assignment={assignment} />
        </>
      ) : (
        <p>
          Your role does not include member read access. Binding counts remain
          available above.
        </p>
      )}
    </section>
  );
}

function RoleMemberList({
  tenant,
  role,
  revision,
  assignment,
  canAssign,
  busy,
  onChange,
}: Props & { assignment: "assigned" | "available" }) {
  const load = useCallback(
    (id: string, filter: DirectoryFilter) =>
      api.roleMembers(id, role.role_id, assignment, filter),
    [role.role_id, assignment],
  );
  const directory = useDirectory(load, tenant, true, revision);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  async function change(member: string) {
    setError("");
    setNotice("");
    const message = await onChange(member, assignment === "available");
    if (message) setError(message);
    else
      setNotice(
        assignment === "available"
          ? "Member assigned to this role."
          : "Member removed from this role.",
      );
    directory.retry();
  }
  return (
    <>
      <p>
        {assignment === "assigned"
          ? "People currently assigned to this role, including suspended members."
          : "Tenant members without this role. Resume suspended members from Members before assigning a role."}
      </p>
      <DirectorySearch
        label="Find a role member"
        placeholder="Email or account ID"
        statuses={["active", "suspended"]}
        onSearch={directory.search}
      />
      <DirectoryNavigation directory={directory} label="Role members" />
      <ErrorMessage message={error} />
      {notice ? <p role="status">{notice}</p> : null}
      {busy ? <p role="status">Saving role membership…</p> : null}
      <div className="table-card">
        <table>
          <thead>
            <tr>
              <th>Member</th>
              <th>Status</th>
              <th>Action</th>
            </tr>
          </thead>
          <tbody>
            {directory.page?.items.map((member) => (
              <tr key={member.account_id}>
                <td>
                  <strong>{member.email}</strong>
                  <small className="block break-text">
                    {member.account_id}
                  </small>
                </td>
                <td>
                  <span
                    className={
                      member.status === "active" ? "pill positive" : "pill"
                    }
                  >
                    {member.status}
                  </span>
                </td>
                <td>
                  {canAssign ? (
                    <button
                      className={
                        assignment === "assigned"
                          ? "danger-button"
                          : "secondary-button"
                      }
                      disabled={
                        busy ||
                        (assignment === "available" &&
                          member.status !== "active")
                      }
                      onClick={() => void change(member.account_id)}
                    >
                      {assignment === "assigned"
                        ? "Remove from role"
                        : "Assign to role"}
                    </button>
                  ) : (
                    <span>Read only</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {directory.page && !directory.page.items.length ? (
          <p className="quiet-state">
            No {assignment} members match these filters.
          </p>
        ) : null}
      </div>
      {canAssign ? (
        <p>
          Each change is saved immediately. Permission coverage and
          last-administrator protections apply.
        </p>
      ) : null}
    </>
  );
}

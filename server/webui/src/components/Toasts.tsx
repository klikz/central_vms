import { useToasts } from "../toast";

export function Toasts() {
  const list = useToasts();
  return (
    <div className="toasts">
      {list.map((x) => (
        <div key={x.id} className={"toast " + (x.kind === "err" ? "err" : x.kind === "warn" ? "warn" : "")}>
          {x.text}
        </div>
      ))}
    </div>
  );
}
